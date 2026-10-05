// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package auth

// SessionKeySubject is where a sign-in flow records the subject the person
// it signed in is authorised as: the id a policy row names. It is the same
// namespace a bearer token's user id and an API key's owner live in, so
// one person reaches the same policy rows whichever credential the request
// carries — a session, a token or a key.
//
// The account flows (pkg/accounts) write the account's id here when they
// start a session; the framework's default-deny layer reads it. The value
// goes away with the session (Destroy) and travels with it when the token
// rotates (RenewToken). A module that signs people in some other way
// records their id here to put them under the same policies.
const SessionKeySubject = "nucleus_subject"
