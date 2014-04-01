// attempt to keep app from going inactive

chrome.alarms.create("ping", {when: 5000, periodInMinutes: 1 });
chrome.alarms.onAlarm.addListener(function(alarm) { console.info("alarm name = " + alarm.name); });

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

chrome.runtime.onMessageExternal.addListener(function(request, header, sendResponse) {
  var timeout = 2000;
  var xhr = new XMLHttpRequest();
  xhr.ontimeout = function() {
    console.error(url + "timed out.");
    chrome.webRequest.onBeforeSendHeaders.removeListener(onBeforeSendHeadersCallback);
  };
  xhr.onerror = function() {
    chrome.webRequest.onBeforeSendHeaders.removeListener(onBeforeSendHeadersCallback);
    var response = { error: xhr.statusText };
    sendResponse(response);
  };
  xhr.onreadystatechange = function() {
    if (xhr.readyState == 4) {
      chrome.webRequest.onBeforeSendHeaders.removeListener(onBeforeSendHeadersCallback);
      var response = {status: xhr.status, body: btoa(xhr.responseText) };
      sendResponse(response);
    }
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
    body = atob(request.body);
    xhr.setRequestHeader("Content-Type", "application/octet-stream");
    console.log(body);
  }

  chrome.webRequest.onBeforeSendHeaders.addListener(onBeforeSendHeadersCallback, {
    urls: [url],
    types: ['xmlhttprequest']
  }, ['requestHeaders', 'blocking']);

  xhr.send(body);
});
