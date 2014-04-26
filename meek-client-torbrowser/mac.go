// +build darwin
// This file is compiled only on mac. It contains paths used by the mac
// browser bundle.
// http://golang.org/pkg/go/build/#hdr-Build_Constraints

package main

const (
	firefoxPath = "../Contents/MacOS/TorBrowser.app/Contents/MacOS/firefox"
	firefoxProfilePath = "../Data/Browser/profile.meek-http-helper"
)
