// +build windows
// This file is compiled only on windows. It contains paths used by the windows
// browser bundle.
// http://golang.org/pkg/go/build/#hdr-Build_Constraints

package main

const (
	firefoxPath        = "./firefox.exe"
	firefoxProfilePath = "TorBrowser/Data/Browser/profile.meek-http-helper"
)
