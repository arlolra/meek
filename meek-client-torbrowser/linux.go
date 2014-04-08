// +build linux
// This file is compiled only on linux. It contains paths used by the linux
// browser bundle.
// http://golang.org/pkg/go/build/#hdr-Build_Constraints

package main

var exitOnStdinEOF = false

var firefoxPath = "Browser/firefox"
var firefoxProfilePath = "Data/Browser/profile.meek-http-helper"
var meekClientPath = "Tor/PluggableTransports/meek-client"
