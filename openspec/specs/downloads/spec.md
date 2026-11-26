# downloads Specification

## Purpose

A GET on a file's URL downloads it. This spec states what a download answers: its bytes, its
headers, and how it answers a Range header.

## Requirements

### Requirement: File download

The system SHALL send a visible file's bytes with the MIME type of its requested name, for a
symlink too, whatever the request's Accept header, and with the same Content-Security-Policy and
`nosniff` headers as every page. The system SHALL neither list nor serve a file it has no
permission to read.

#### Scenario: A file downloads with its bytes and MIME type

- **WHEN** a client requests `/a.txt`
- **THEN** the answer is 200 with the file's bytes and `content-type: text/plain`
- **AND** it carries `x-content-type-options: nosniff` and the `default-src 'self'` policy

#### Scenario: A download ignores the Accept header

- **WHEN** a client requests a file with `Accept: application/json`
- **THEN** the answer is 200 with the file's bytes

#### Scenario: A symlink downloads with the MIME type of its own name

- **WHEN** `x.txt` is a symlink to `x.html`
- **THEN** a GET for `/x.txt` answers `content-type: text/plain`

#### Scenario: A file that cannot be read answers 404 and is not listed

- **WHEN** a file exists and the server has no permission to read it
- **THEN** a GET for it answers 404
- **AND** the listing of its directory does not link it

#### Scenario: A served file is not shadowed by the app's own static files

- **WHEN** the root holds a file whose name the app also ships, such as `favicon.ico`
- **THEN** a GET for it answers with the root's file, because the app's own files live under `/.app`

### Requirement: Range requests

A file download SHALL answer one `bytes=a-b`, `bytes=a-` or `bytes=-n` range with a 206 and its
`Content-Range`, and an unsatisfiable one with a 416 and `Content-Range: bytes */<size>`. It SHALL
answer several ranges, a malformed Range header or an empty file with the full 200. Every
download SHALL carry `Accept-Ranges: bytes`.

#### Scenario: A single range answers 206 with exactly its bytes

- **WHEN** a client requests a ten-byte file with `bytes=2-5`, `bytes=7-`, `bytes=-3`, a range
  that runs past the end, or a suffix longer than the file
- **THEN** the answer is 206 with exactly the bytes of the range, clipped to the file
- **AND** it carries the matching `Content-Range` and `Accept-Ranges: bytes`

#### Scenario: A range past the end answers 416 with the size

- **WHEN** a client asks a ten-byte file for `bytes=10-`, `bytes=20-30` or `bytes=-0`
- **THEN** the answer is 416 with `Content-Range: bytes */10` and `Accept-Ranges: bytes`

#### Scenario: No range, several ranges or a malformed one answers the full 200

- **WHEN** a client sends no Range header, two ranges in one header, a range that does not parse,
  a range whose end comes before its start, or a unit other than bytes
- **THEN** the answer is 200 with the whole file, `Accept-Ranges: bytes` and no `Content-Range`

#### Scenario: Two Range headers answer the full 200

- **WHEN** a request carries two Range headers
- **THEN** the answer is 200 with the whole file

#### Scenario: An empty file answers the full 200 whatever the range

- **WHEN** a client requests an empty file with `bytes=0-`
- **THEN** the answer is 200 with an empty body
