package server

import _ "embed"

// The fixture contains a blank 1 MiB virtual disk. It can be recreated with
// qemu-img create -f qcow2 -o compat=1.1,cluster_size=512 fixtures/blank.qcow2 1M
//
//go:embed fixtures/blank.qcow2
var fixtureImageData string
