// https://developer.mozilla.org/en-US/docs/How_to_Build_an_XPCOM_Component_in_Javascript#Using_XPCOMUtils
// https://developer.mozilla.org/en-US/docs/Mozilla/JavaScript_code_modules/XPCOMUtils.jsm
Components.utils.import("resource://gre/modules/XPCOMUtils.jsm");

function MeekHTTPHelper() {
    this.wrappedJSObject = this;

    const LOCAL_PORT = 7000;

    // Create a "direct" nsIProxyInfo that bypasses the default proxy.
    // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIProtocolProxyService
    var pps = Components.classes["@mozilla.org/network/protocol-proxy-service;1"]
        .getService(Components.interfaces.nsIProtocolProxyService);
    // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIProxyInfo
    this.directProxyInfo = pps.newProxyInfo("direct", "", 0, 0, 0xffffffff, null);

    // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIIOService
    this.ioService = Components.classes["@mozilla.org/network/io-service;1"]
        .getService(Components.interfaces.nsIIOService);
    this.httpProtocolHandler = this.ioService.getProtocolHandler("http")
        .QueryInterface(Components.interfaces.nsIHttpProtocolHandler);

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
    ]),

    // nsIServerSocketListener implementation.
    onSocketAccepted: function(aServ, aTransport) {
        dump("onSocketAccepted host " + aTransport.host + "\n");

        const FRONT_URL = "https://www.google.com/";
        const HOST = "meek-reflect.appspot.com";

        var uri = this.ioService.newURI(FRONT_URL, null, null);
        // Construct an HTTP channel with the proxy bypass.
        // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIHttpChannel
        var channel = this.httpProtocolHandler.newProxiedChannel(uri, this.directProxyInfo, 0, null)
            .QueryInterface(Components.interfaces.nsIHttpChannel);
        // Set the host we really want.
        channel.setRequestHeader("Host", HOST, false);
        channel.redirectionLimit = 0;
        // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIUploadChannel
        // channel.requestMethod = "POST";

        channel.asyncOpen(new this.httpResponseStreamListener(), null);
    },
    onStopListening: function(aServ, aStatus) {
        dump("onStopListening status " + aStatus + "\n");
    },
};


// https://developer.mozilla.org/en-US/docs/Creating_Sandboxed_HTTP_Connections
MeekHTTPHelper.prototype.httpResponseStreamListener = function() {
}
MeekHTTPHelper.prototype.httpResponseStreamListener.prototype = {
    // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIRequestObserver
    onStartRequest: function(aRequest, aContext) {
        dump("onStartRequest\n");
    },
    onStopRequest: function(aRequest, aContext, aStatus) {
        dump("onStopRequest\n");
    },
    // https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIStreamListener
    onDataAvailable: function(aRequest, aContext, aStream, aSourceOffset, aLength) {
        dump("onDataAvailable\n");
        var a = new Uint8Array(aLength);
        var input = Components.classes["@mozilla.org/binaryinputstream;1"]
            .createInstance(Components.interfaces.nsIBinaryInputStream);
        input.setInputStream(aStream);
        input.readByteArray(aLength, a);
        dump(aLength + ":" + a + "\n");
    },
};

var NSGetFactory = XPCOMUtils.generateNSGetFactory([MeekHTTPHelper]);
