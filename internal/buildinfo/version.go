// Package buildinfo provides the agent release identity, independently of Xray
// and durable state/API schema versions.
package buildinfo

const Version = "1.0.1-audit3-test"
const BaseVersion = "v1.0.1"
const Tag = "v" + Version
const UserAgent = "ZIC-Xray-Agent/" + Version
