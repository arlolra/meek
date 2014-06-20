// +build darwin
// This file is compiled only on mac. It contains paths used by the mac
// browser bundle.
// http://golang.org/pkg/go/build/#hdr-Build_Constraints

package main

const (
	// The TorBrowser.app.meek-http-helper directory is a special case for
	// the mac bundle. It is a copy of TorBrowser.app that has a modified
	// Info.plist file so that it doesn't show a dock icon.
	firefoxPath        = "PluggableTransports/TorBrowser.app.meek-http-helper/Contents/MacOS/firefox"
	firefoxProfilePath = "../Data/Browser/profile.meek-http-helper"
)
