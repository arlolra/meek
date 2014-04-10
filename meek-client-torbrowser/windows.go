// +build windows
// This file is compiled only on windows. It contains paths used by the windows
// browser bundle.
// http://golang.org/pkg/go/build/#hdr-Build_Constraints

package main

// Workaround for process termination on Windows only.
var exitOnStdinEOF = true

var firefoxPath string = "Browser/firefox.exe"
var firefoxProfilePath = "Data/Browser/profile.meek-http-helper"
