// Package anna provides an Anna's Archive client for search, record details,
// DOI lookup, account status, public file statistics, and verified downloads.
//
// NewClient accepts explicit Config values, validates malformed configuration,
// and makes no network requests. It does not read environment variables or
// choose home-directory paths. Missing credentials and a missing download
// directory are checked only when an operation needs them. SessionFile is
// optional; empty disables session and mirror persistence. A nil Logger is silent.
//
// A positive operation timeout adds a context deadline. Zero adds no operation
// deadline; request header, body idle, sign-in, and DOI lookup limits still
// apply. FileStats uses its own bounded budget plus the caller's context.
//
// Clients support concurrent operations, coordinating sign-in, mirror health,
// statistics budgets/cache, and downloads for the same hash within one client.
// Callbacks run synchronously during transfers and must return promptly. A
// callback shared across concurrent operations must synchronize its own state.
// Reused local files do not produce transfer progress. Separate clients and
// processes do not share download locks; persisted state is best effort.
//
// Errors retain machine-readable categories through CodeOf and wrapped causes
// through errors.Is and errors.As. SearchPage distinguishes exact and partial
// results, lower-bound totals, and pagination; optional statistics distinguish
// an unknown value (nil or absent) from a confirmed zero. JSON tags on public
// domain records are stable; signed file URLs and credentials are not serialized.
package anna
