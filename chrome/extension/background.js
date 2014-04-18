const DEBUG = false;

function debug(str) {
  if (DEBUG) { console.info(str); }
}

chrome.alarms.create("heartbeat", {when: 5000, periodInMinutes: 1 });
chrome.alarms.onAlarm.addListener(function(alarm) { 
  chrome.management.getAll(function(extensions) {
    var app_id;
    for (var i = 0; i < extensions.length; i++) {
      if (extensions[i].name === "meek-browser-app") {
        app_id = extensions[i].id;
        break;
      }
    }
    chrome.runtime.sendMessage(app_id, chrome.runtime.id);
  });
});

var host = 'meek-reflect.appspot.com';

function onBeforeSendHeadersCallback(details) {
  var did_set = false;
  for (var i = 0; i < details.requestHeaders.length; ++i) {
    if (details.requestHeaders[i].name === 'Host') {
      details.requestHeaders[i].value = host;
      did_set = true;
    }
  }
  if (!did_set) {
    details.requestHeaders.push({
      name: 'Host',
      value: host
    });
  }
  return { requestHeaders: details.requestHeaders };
}

chrome.runtime.onConnectExternal.addListener(function(port) {
  debug("onConnectExternal");
  port.onMessage.addListener(function(request) {
    debug("onMessage");
    var timeout = 2000;
    var xhr = new XMLHttpRequest();
    xhr.responseType = "arraybuffer";
    xhr.ontimeout = function() {
      console.error(url + "timed out.");
      chrome.webRequest.onBeforeSendHeaders.removeListener(onBeforeSendHeadersCallback);
    };
    xhr.onerror = function() {
      chrome.webRequest.onBeforeSendHeaders.removeListener(onBeforeSendHeadersCallback);
      var response = { error: xhr.statusText };
      sendResponse(response);
    };
    xhr.onload = function() {
      debug("onload " + xhr.response.byteLength);
      chrome.webRequest.onBeforeSendHeaders.removeListener(onBeforeSendHeadersCallback);
      var response = {
        status: xhr.status,
        body: _arrayBufferToBase64(xhr.response)
      };
      port.postMessage(response);
      debug("postMessage " + JSON.stringify(response));
    };
    var requestMethod = request.method;
    var url = request.url;
    xhr.open(requestMethod, url);
    if (request.header != undefined) {
      for (var key in request.header) {
        if (key != "Host") { // TODO: Add more restricted header fields
          xhr.setRequestHeader(key, request.header[key]);
        } else {
          host = request.header[key];
        }
      }
    }
    var body = null;
    if (request.body != undefined) {
      body = _base64ToArrayBuffer(request.body);
      xhr.overrideMimeType("Content-Type", "application/octet-stream");
      debug(body);
    }

    chrome.webRequest.onBeforeSendHeaders.addListener(onBeforeSendHeadersCallback, {
      urls: [url],
      types: ['xmlhttprequest']
    }, ['requestHeaders', 'blocking']);

    xhr.send(body);
  });
});

function _base64ToArrayBuffer(base64) {
  var binary_string = atob(base64);
  var len = binary_string.length;
  var bytes = new Uint8Array(len);
  for (var i = 0; i < len; i++) {
    bytes[i] = binary_string.charCodeAt(i);
  }
  return bytes;
}

function _arrayBufferToBase64(buf) {
  var bytes = new Uint8Array(buf);
  debug(JSON.stringify(buf));
  var base64    = '';
  var encodings = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=';
  var chr1, chr2, chr3, enc1, enc2, enc3, enc4;
  var i = 0;
  while (i < bytes.length) {
    chr1 = bytes[i++];
    chr2 = i < bytes.length ? bytes[i++] : Number.NaN;
    chr3 = i < bytes.length ? bytes[i++] : Number.NaN;

    enc1 = chr1 >> 2;
    enc2 = ((chr1 & 3) << 4) | (chr2 >> 4);
    enc3 = ((chr2 & 15) << 2) | (chr3 >> 6);
    enc4 = chr3 & 63;

    if (isNaN(chr2)) {
      enc3 = enc4 = 64;
    } else if (isNaN(chr3)) {
      enc4 = 64;
    }
    base64 += encodings.charAt(enc1) + encodings.charAt(enc2) +
              encodings.charAt(enc3) + encodings.charAt(enc4);
  }
  return base64;
}
