// Copyright (C) 2026 The Nebla contributors
// SPDX-License-Identifier: Apache-2.0

// Package nebla is the Go SDK for the Nebla panel API.
//
// Create a Client with NewClient, passing WithEndpoint and WithToken.
// Each resource is a service field on the Client, such as Servers or Actions.
// Requests that return an action can be awaited with Actions.WaitFor.
package nebla
