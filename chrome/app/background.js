const DEBUG = false;

function debug(str) {
  if (DEBUG) { console.debug(str); }
}

function info(str) {
  console.info(str);
}

const IP = "127.0.0.1";
const PORT = 7000;

const STATE_READING_LENGTH = 1;
const STATE_READING_OBJECT = 2;

var serverSocketId;
var state = STATE_READING_LENGTH;
var buf = new Uint8Array(4);
var bytesToRead = buf.length;

chrome.runtime.onMessageExternal.addListener(
  function onHeartbeat(id, sender, sendResponse) {
    console.assert(id === sender.id, "Sender's ID is incorrect.");
    EXTENSION_ID = id;
    chrome.runtime.onMessageExternal.removeListener(onHeartbeat);
    chrome.sockets.tcpServer.create({}, function(createInfo) {
      listenAndAccept(createInfo.socketId);
    });
  }
);

function listenAndAccept(socketId) {
  info("listenAndAccept " + socketId);
  chrome.sockets.tcpServer.listen(socketId,
    IP, PORT, function(resultCode) {
      onListenCallback(socketId, resultCode)
  });
}

function onListenCallback(socketId, resultCode) {
  debug("onListenCallback " + socketId);
  if (resultCode < 0) {
    debug("Error listening:" +
      chrome.runtime.lastError.message);
    return;
  }
  serverSocketId = socketId;
  chrome.sockets.tcpServer.onAccept.addListener(onAccept);
  chrome.sockets.tcpServer.onAcceptError.addListener(function(info) {
    debug("onAcceptError " + JSON.stringify(info));
  });
  chrome.sockets.tcp.onReceive.addListener(onReceive);
  chrome.sockets.tcp.onReceiveError.addListener(function(info) {
    chrome.sockets.tcp.close(info.socketId);
    debug("onReceiveError " + JSON.stringify(info));
  });
}

function onAccept(info) {
  debug("onAccept " + JSON.stringify(info));
  if (info.socketId != serverSocketId)
    return;

  chrome.sockets.tcp.setPaused(info.clientSocketId, false);
}

function readIntoBuf(data) {
  debug("readIntoBuf " + "bytesToRead: " + bytesToRead + ", datalen: " + data.byteLength + ", buflen: " + buf.length);
  var n = Math.min(data.byteLength, bytesToRead);
  buf.set(new Uint8Array(data.slice(0, n)), buf.length - bytesToRead);
  bytesToRead -= n;
  return data.slice(n);
}

function onReceive(info) {
  debug("onReceive " + JSON.stringify(info) + " len: " + info.data.byteLength);
  var data = info.data;
  switch (state) {
  case STATE_READING_LENGTH:
    data = readIntoBuf(data);
    if (bytesToRead > 0)
      return;

    var b = buf;
    bytesToRead = (b[0] << 24) | (b[1] << 16) | (b[2] << 8) | b[3];
    debug(bytesToRead);
    buf = new Uint8Array(bytesToRead);
    state = STATE_READING_OBJECT;

  case STATE_READING_OBJECT:
    data = readIntoBuf(data);
    if (bytesToRead > 0)
      return;

    var str = ab2str(buf);
    debug(str);
    var request = JSON.parse(str);
    makeRequest(request, info.socketId);

    state = STATE_READING_LENGTH;
    buf = new Uint8Array(4);
    bytesToRead = buf.length;
  }
}

function makeRequest(request, socketId) {
  debug("makeRequest " + JSON.stringify(request));

  port = chrome.runtime.connect(EXTENSION_ID);
  port.onMessage.addListener(function(response) {
    returnResponse(response, socketId);
    port.disconnect();
  });
  port.onDisconnect.addListener(function() {
    debug("onDisconnect");
  });
  port.postMessage(request);
}

function returnResponse(response, socketId) {
  debug("returnResponse " + JSON.stringify(response));
  var str = JSON.stringify(response);
  var b = str2ab(str);

  var buf = new Uint8Array(4 + b.byteLength);
  var len = b.byteLength;
  buf[0] = (len >> 24) & 0xff;
  buf[1] = (len >> 16) & 0xff;
  buf[2] = (len >> 8) & 0xff;
  buf[3] = len & 0xff;
  buf.set(new Uint8Array(b), 4);

  chrome.sockets.tcp.send(socketId, buf.buffer, function(info) {
    debug("send " + socketId);
    if (info.resultCode != 0)
      debug("Send failed " + info.resultCode);
  });
}

function ab2str(buffer) {
  var encodedString = String.fromCharCode.apply(null, buffer),
      decodedString = decodeURIComponent(escape(encodedString));
  return decodedString;
}

function str2ab(string) {
    var string = unescape(encodeURIComponent(string)),
        charList = string.split(''),
        buf = [];
    for (var i = 0; i < charList.length; i++) {
      buf.push(charList[i].charCodeAt(0));
    }
    return (new Uint8Array(buf)).buffer;
}
