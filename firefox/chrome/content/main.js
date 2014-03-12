var FRONT_URL = "https://www.google.com/";
var HOST = "meek-reflect.appspot.com";

// Create a "direct" nsIProxyInfo that bypasses the default proxy.
// https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIProtocolProxyService
var pps = Components.classes["@mozilla.org/network/protocol-proxy-service;1"]
	.getService(Components.interfaces.nsIProtocolProxyService);
// https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIProxyInfo
var proxy = pps.newProxyInfo("direct", "", 0, 0, 0xffffffff, null);

// https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIIOService
var ioService = Components.classes["@mozilla.org/network/io-service;1"]
	.getService(Components.interfaces.nsIIOService);
var httpProtocolHandler = ioService.getProtocolHandler("http")
	.QueryInterface(Components.interfaces.nsIHttpProtocolHandler);
var uri = ioService.newURI(FRONT_URL, null, null);
// Construct an HTTP channel with the proxy bypass.
// https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIHttpChannel
var channel = httpProtocolHandler.newProxiedChannel(uri, proxy, 0, null)
	.QueryInterface(Components.interfaces.nsIHttpChannel);
// Set the host we really want.
channel.setRequestHeader("Host", HOST, false);
channel.redirectionLimit = 0;
// https://developer.mozilla.org/en-US/docs/XPCOM_Interface_Reference/nsIUploadChannel
// channel.requestMethod = "POST";

var listener = new StreamListener();
channel.asyncOpen(listener, null);

// https://developer.mozilla.org/en-US/docs/Creating_Sandboxed_HTTP_Connections
function StreamListener() {
	this.onStartRequest = function(aRequest, aContext) {
		dump("onStartRequest\n");
	};
	this.onStopRequest = function(aRequest, aContext, aStatus) {
		dump("onStopRequest\n");
	};
	this.onDataAvailable = function(aRequest, aContext, aStream, aSourceOffset, aLength) {
		dump("onDataAvailable\n");
		var a = new Uint8Array(aLength);
		var input = Components.classes["@mozilla.org/binaryinputstream;1"]
			.createInstance(Components.interfaces.nsIBinaryInputStream);
		input.setInputStream(aStream);
		input.readByteArray(aLength, a);
		dump(aLength + ":" + a + "\n");
	};
}
