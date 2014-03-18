// This is an extension that allows external programs to make HTTP requests
// using the browser's networking libraries.
//
// The extension opens a TCP socket listening on localhost (port 7000). When it
// receives a connection, it reads a 4-byte big-endian length field, then tries
// to read that many bytes of data. The data is UTF-8–encoded JSON, having the
// format
//  {
//      "method": "POST",
//      "url": "https://www.google.com/",
//      "header": {
//          "Host": "meek-reflect.appspot.com",
//          "X-Session-Id": "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"}
//      }
//  }
// The extension makes the request as commanded. It returns the response to the
// client as a JSON blob, preceded by a 4-byte length as before. If successful,
// the response looks like
//  {
//      "status": 200,
//      "body": "...base64..."
//  }
// If there is a network error, the "error" key will be defined. A 404 response
// or similar from the target web server is not considered such an error.
//  {
//      "error": "NS_ERROR_UNKNOWN_HOST"
//  }
// The extension closes the connectionn after each transaction, and the client
// must reconnect to do another request.

// https://developer.mozilla.org/en-US/docs/How_to_Build_an_XPCOM_Component_in_Javascript#Using_XPCOMUtils
// https://developer.mozilla.org/en-US/docs/Mozilla/JavaScript_code_modules/XPCOMUtils.jsm
Components.utils.import("resource://gre/modules/XPCOMUtils.jsm");

// Everything resides within the MeekHTTPHelper namespace. MeekHTTPHelper is
// also the type from which NSGetFactory is constructed, and it is the top-level
// nsIServerSocketListener.
function MeekHTTPHelper() {
    this.wrappedJSObject = this;

    const LOCAL_PORT = 7000;

    // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIServerSocket
    var serverSocket = Components.classes["@mozilla.org/network/server-socket;1"]
        .createInstance(Components.interfaces.nsIServerSocket);
    serverSocket.init(LOCAL_PORT, true, -1);
    serverSocket.asyncListen(this);
}
MeekHTTPHelper.prototype = {
    classDescription: "meek HTTP helper component",
    classID: Components.ID("{e7bc2b9c-f454-49f3-a19f-14848a4d871d}"),
    contractID: "@bamsoftware.com/meek-http-helper;1",

    // https://developer.mozilla.org/en-US/docs/Mozilla/JavaScript_code_modules/XPCOMUtils.jsm#generateQI%28%29
    QueryInterface: XPCOMUtils.generateQI([
        Components.interfaces.nsIServerSocketListener,
        Components.interfaces.nsIObserver,
    ]),

    // nsIServerSocketListener implementation.
    onSocketAccepted: function(server, transport) {
        dump("onSocketAccepted " + transport.host + ":" + transport.port + "\n");
        new MeekHTTPHelper.LocalConnectionHandler(transport);
    },
    onStopListening: function(server, status) {
        dump("onStopListening status " + status + "\n");
    },
};

// Global variables and functions.

MeekHTTPHelper.LOCAL_READ_TIMEOUT = 2.0;
MeekHTTPHelper.LOCAL_WRITE_TIMEOUT = 2.0;

// A "direct" nsIProxyInfo that bypasses the default proxy.
// https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIProtocolProxyService
// https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIProxyInfo
MeekHTTPHelper.directProxyInfo = Components.classes["@mozilla.org/network/protocol-proxy-service;1"]
    .getService(Components.interfaces.nsIProtocolProxyService)
    .newProxyInfo("direct", "", 0, 0, 0xffffffff, null);

// https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIIOService
MeekHTTPHelper.ioService = Components.classes["@mozilla.org/network/io-service;1"]
    .getService(Components.interfaces.nsIIOService);
MeekHTTPHelper.httpProtocolHandler = MeekHTTPHelper.ioService.getProtocolHandler("http")
    .QueryInterface(Components.interfaces.nsIHttpProtocolHandler);

// Set the transport to time out at the given absolute deadline.
MeekHTTPHelper.refreshDeadline = function(transport, deadline) {
    var timeout;
    if (deadline === null)
        timeout = 0xffffffff;
    else
        timeout = Math.max(0.0, Math.ceil((deadline - Date.now()) / 1000.0));
    transport.setTimeout(Components.interfaces.nsISocketTransport.TIMEOUT_READ_WRITE, timeout);
};

// Reverse-index the Components.results table.
MeekHTTPHelper.lookupStatus = function(status) {
    for (var name in Components.results) {
        if (Components.results[name] === status)
            return name;
    }
    return null;
};

// LocalConnectionHandler handles each new client connection received on the
// socket opened by MeekHTTPHelper. It reads a JSON request, makes the request
// on the Internet, and writes the result back to the socket. Error handling
// happens within callbacks.
MeekHTTPHelper.LocalConnectionHandler = function(transport) {
    dump("LocalConnectionHandler\n");
    this.transport = transport;
    this.readRequest(this.makeRequest.bind(this));
};
MeekHTTPHelper.LocalConnectionHandler.prototype = {
    readRequest: function(callback) {
        dump("readRequest\n");
        new MeekHTTPHelper.RequestReader(this.transport, this.makeRequest.bind(this));
    },

    makeRequest: function(req) {
        dump("makeRequest " + JSON.stringify(req) + "\n");
        if (!this.requestOk(req)) {
            this.transport.close(0);
            return;
        }
        var uri = MeekHTTPHelper.ioService.newURI(req.url, null, null);
        // Construct an HTTP channel with the proxy bypass.
        // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIHttpChannel
        var channel = MeekHTTPHelper.httpProtocolHandler.newProxiedChannel(uri, MeekHTTPHelper.directProxyInfo, 0, null)
            .QueryInterface(Components.interfaces.nsIHttpChannel);
        if (req.header !== undefined) {
            for (var key in req.header) {
                dump("setting header " + key + ": " + req.header[key] + "\n");
                channel.setRequestHeader(key, req.header[key], false);
            }
        }
        if (req.body !== undefined) {
            let body = atob(req.body);
            let inputStream = Components.classes["@mozilla.org/io/string-input-stream;1"]
                .createInstance(Components.interfaces.nsIStringInputStream);
            inputStream.setData(body, body.length);
            let uploadChannel = channel.QueryInterface(Components.interfaces.nsIUploadChannel);
            uploadChannel.setUploadStream(inputStream, "application/octet-stream", body.length);
        }
        // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIUploadChannel
        // says we must set requestMethod after calling setUploadStream.
        channel.requestMethod = req.method;
        channel.redirectionLimit = 0;

        channel.asyncOpen(new MeekHTTPHelper.HttpStreamListener(this.returnResponse.bind(this)), channel);
    },

    returnResponse: function(resp) {
        dump("returnResponse " + JSON.stringify(resp) + "\n");
        outputStream = this.transport.openOutputStream(
            Components.interfaces.nsITransport.OPEN_BLOCKING | Components.interfaces.nsITransport.OPEN_UNBUFFERED, 0, 0);
        var output = Components.classes["@mozilla.org/binaryoutputstream;1"]
            .createInstance(Components.interfaces.nsIBinaryOutputStream);
        output.setOutputStream(outputStream);

        var converter = Components.classes["@mozilla.org/intl/scriptableunicodeconverter"]
            .createInstance(Components.interfaces.nsIScriptableUnicodeConverter);
        converter.charset = "UTF-8";
        var s = JSON.stringify(resp);
        var data = converter.convertToByteArray(s);

        var deadline = Date.now() + MeekHTTPHelper.LOCAL_WRITE_TIMEOUT * 1000;
        try {
            MeekHTTPHelper.refreshDeadline(this.transport, deadline);
            output.write32(data.length);
            MeekHTTPHelper.refreshDeadline(this.transport, deadline);
            output.writeByteArray(data, data.length);
        } finally {
            output.close();
        }
    },

    // Enforce restrictions on what requests we are willing to make. These can
    // probably be loosened up. Try and rule out anything unexpected until we
    // know we need otherwise.
    requestOk: function(req) {
        if (req.method === undefined) {
            dump("req missing \"method\".\n");
            return false;
        }
        if (req.url === undefined) {
            dump("req missing \"url\".\n");
            return false;
        }

        if (req.method !== "POST") {
            dump("req.method is " + JSON.stringify(req.method) + ", not \"POST\".\n");
            return false;
        }
        if (!req.url.startsWith("https://")) {
            dump("req.url doesn't start with \"https://\".\n");
            return false;
        }

        return true;
    },
};

// RequestReader reads a JSON-encoded request from the given transport, and
// calls the given callback with the request as an argument. In case of error,
// the transport is closed and the callback is not called.
MeekHTTPHelper.RequestReader = function(transport, callback) {
    dump("RequestReader\n");
    this.transport = transport;
    this.callback = callback;

    this.curThread = Components.classes["@mozilla.org/thread-manager;1"].getService().currentThread;
    this.inputStream = this.transport.openInputStream(
        Components.interfaces.nsITransport.OPEN_BLOCKING | Components.interfaces.nsITransport.OPEN_UNBUFFERED, 0, 0);

    this.state = this.STATE_READING_LENGTH;
    this.buf = new Uint8Array(4);
    this.bytesToRead = this.buf.length;
    this.deadline = Date.now() + MeekHTTPHelper.LOCAL_READ_TIMEOUT * 1000;
    this.asyncWait();
};
MeekHTTPHelper.RequestReader.prototype = {
    STATE_READING_LENGTH: 1,
    STATE_READING_OBJECT: 2,
    STATE_DONE: 3,

    // Do an asyncWait and handle the result.
    asyncWait: function() {
        MeekHTTPHelper.refreshDeadline(this.transport, this.deadline);
        this.inputStream.asyncWait(this, 0, 0, this.curThread);
    },

    // nsIInputStreamCallback implementation.
    onInputStreamReady: function(inputStream) {
        var input = Components.classes["@mozilla.org/binaryinputstream;1"]
            .createInstance(Components.interfaces.nsIBinaryInputStream);
        input.setInputStream(inputStream);
        try {
            switch (this.state) {
            case this.STATE_READING_LENGTH:
                this.doStateReadingLength(input);
                this.asyncWait(inputStream);
                break;
            case this.STATE_READING_OBJECT:
                this.doStateReadingObject(input);
                if (this.bytesToRead > 0)
                    this.asyncWait(inputStream);
                break;
            }
        } catch (e) {
            dump("got exception " + e + "\n");
            this.transport.close(0);
            return;
        }
    },

    // Read into this.buf (up to its capacity) and decrement this.bytesToRead.
    readIntoBuf: function(input) {
        var n = Math.min(input.available(), this.bytesToRead);
        var data = input.readByteArray(n);
        this.buf.subarray(this.buf.length - this.bytesToRead, n).set(data)
        this.bytesToRead -= n;
    },

    doStateReadingLength: function(input) {
        this.readIntoBuf(input);
        if (this.bytesToRead > 0)
            return;

        this.state = this.STATE_READING_OBJECT;
        var b = this.buf;
        this.bytesToRead = (b[0] << 24) | (b[1] << 16) | (b[2] << 8) | b[3];
        if (this.bytesToRead > 1000000) {
            dump("Object length is too long (" + this.bytesToRead + "), ignoring.\n");
            throw Components.results.NS_ERROR_FAILURE;
        }
        this.buf = new Uint8Array(this.bytesToRead);
    },

    doStateReadingObject: function(input) {
        this.readIntoBuf(input);
        if (this.bytesToRead > 0)
            return;

        this.state = this.STATE_DONE;
        var converter = Components.classes["@mozilla.org/intl/scriptableunicodeconverter"]
            .createInstance(Components.interfaces.nsIScriptableUnicodeConverter);
        converter.charset = "UTF-8";
        var s = converter.convertFromByteArray(this.buf, this.buf.length);
        var req = JSON.parse(s);
        this.callback(req);
    },
};

// HttpStreamListener makes the requested HTTP request and calls the given
// callback with a representation of the response. The "error" key of the return
// value is defined if and only if there was an error.
MeekHTTPHelper.HttpStreamListener = function(callback) {
    this.callback = callback;
    // This is a list of binary strings that is concatenated in onStopRequest.
    this.body = [];
    this.length = 0;
};
// https://developer.mozilla.org/en-US/docs/Creating_Sandboxed_HTTP_Connections
MeekHTTPHelper.HttpStreamListener.prototype = {
    // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIRequestObserver
    onStartRequest: function(req, context) {
        dump("onStartRequest\n");
    },
    onStopRequest: function(req, context, status) {
        dump("onStopRequest " + status + "\n");
        var resp = {
            status: context.responseStatus,
        };
        if (Components.isSuccessCode(status)) {
            resp.body = btoa(this.body.join(""));
        } else {
            let err = MeekHTTPHelper.lookupStatus(status);
            if (err !== null)
                resp.error = err;
            else
                resp.error = "error " + String(status);
        }
        this.callback(resp);
    },

    // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIStreamListener
    onDataAvailable: function(request, context, stream, sourceOffset, length) {
        dump("onDataAvailable " + length + " bytes\n");
        if (this.length + length > 1000000) {
            request.cancel(Components.results.NS_ERROR_ILLEGAL_VALUE);
            return;
        }
        this.length += length;
        var input = Components.classes["@mozilla.org/binaryinputstream;1"]
            .createInstance(Components.interfaces.nsIBinaryInputStream);
        input.setInputStream(stream);
        this.body.push(String.fromCharCode.apply(null, input.readByteArray(length)));
    },
};

var NSGetFactory = XPCOMUtils.generateNSGetFactory([MeekHTTPHelper]);
