# visibility Specification

## Purpose

SlimServe serves one root and only part of it: nothing outside the root, no dot-file, and nothing
a `.slimserveignore` file hides. This spec states which paths are visible, and that every other
path answers alike.

## Requirements

### Requirement: Containment

The system SHALL answer 404 for any path that resolves outside the root, including through a
symlink or an encoded slash, and for any path with a component that is not valid UTF-8 or holds a
NUL byte.

#### Scenario: A path with .. segments answers 404

- **WHEN** a client requests `/../etc/passwd`, or a `..` path that names an existing file above the root
- **THEN** the answer is 404

#### Scenario: An encoded slash answers 404

- **WHEN** a path segment contains `%2F`, as in `/sub%2F.env`
- **THEN** the answer is 404

#### Scenario: A NUL byte in a path answers 404

- **WHEN** a client requests `/a.txt%00.html`
- **THEN** the answer is 404

#### Scenario: A name that is not valid UTF-8 answers 404

- **WHEN** a client requests a file name that holds the invalid byte `%FF`, and a file of that name
  exists, with or without an ignore file above it
- **THEN** the answer is 404

#### Scenario: A symlink that leaves the root answers 404

- **WHEN** a symlink inside the root points outside it, by a relative or an absolute target
- **THEN** a GET for the link, or for a path through it, answers 404

#### Scenario: A relative symlink that stays inside the root is served

- **WHEN** a relative symlink points to a file or a directory inside the root
- **THEN** a GET for the link, or for a file through it, answers 200 with the target's bytes

#### Scenario: The canary downloads when its own directory is the root

- **WHEN** the directory above the root, which holds the file the traversal scenarios reach for,
  is itself served as the root
- **THEN** a GET for that file answers 200, so those scenarios cannot pass because the file is missing

#### Scenario: Any request path resolves only to a visible path under the root

- **WHEN** a request names any sequence of segments, built from visible names, dot-names, links
  inside and outside the root, `.` and `..`, slashes, encoded slashes, NUL bytes, invalid UTF-8 and
  random bytes
- **THEN** it resolves to nothing, or to a real path under the root with no dot-component and no
  ignored name

### Requirement: Dot-files

The system SHALL neither list nor serve a path with a component starting with ".", whether that
component is in the requested path or in the path a symlink resolves it to.

#### Scenario: A path with a dot component answers 404

- **WHEN** a client requests an existing dot-file, a dot-directory or a file under one
- **THEN** the answer is 404

#### Scenario: A symlink to a dot-file answers 404

- **WHEN** a symlink with a visible name points to a dot-file or a dot-directory inside the root
- **THEN** a GET for the link, or for a path through it, answers 404

### Requirement: Ignore files

The system SHALL neither list nor serve a path that a `.slimserveignore` on its ancestry ignores,
or that lies under an ignored directory, by its requested or its resolved path. An unreadable or
non-UTF-8 ignore file SHALL ignore its whole directory. Patterns are the gitignore subset `*`, `?`,
`**`, a leading or inner `/` to anchor, a trailing `/` for directories and a leading `!` to negate,
and the last match wins.

#### Scenario: A nested ignore file hides a file below it only

- **WHEN** `sub/.slimserveignore` names `secret.txt`
- **THEN** `/sub/secret.txt` and `/sub/deep/secret.txt` answer 404
- **AND** `/sub/open.txt` and the root's own `/secret.txt` answer 200

#### Scenario: An ignored directory hides everything under it

- **WHEN** the root's ignore file holds `private/`
- **THEN** `/private` and `/private/x.txt` answer 404
- **AND** a file named `private` answers 200

#### Scenario: A negated pattern serves the file it names

- **WHEN** the root's ignore file holds `*.txt` and then `!keep.txt`
- **THEN** `/keep.txt` answers 200 and `/a.txt` answers 404

#### Scenario: A negated pattern cannot serve a file under an ignored directory

- **WHEN** the root's ignore file holds `private/` and then `!private/keep.txt`
- **THEN** `/private/keep.txt` answers 404

#### Scenario: A deeper ignore file has the last word

- **WHEN** the root's ignore file holds `*.txt` and `sub/.slimserveignore` holds `!keep.txt`
- **THEN** `/sub/keep.txt` answers 200 and `/sub/other.txt` answers 404

#### Scenario: A symlink into an ignored directory answers 404

- **WHEN** the root's ignore file holds `private/` and symlinks with visible names point to `private` and to a file under it
- **THEN** a GET for either link, or for a path through the directory link, answers 404

#### Scenario: An ignore file that is not valid UTF-8 hides its whole directory

- **WHEN** `bad/.slimserveignore` holds bytes that are not valid UTF-8
- **THEN** `/bad` and `/bad/x.txt` answer 404

#### Scenario: An ignore file that starts with a byte-order mark still applies

- **WHEN** the root's ignore file starts with a UTF-8 byte-order mark and then holds `secret.txt`
- **THEN** `/secret.txt` answers 404 and the root's listing does not link it

#### Scenario: An unreadable ignore file hides its whole directory

- **WHEN** `locked/.slimserveignore` exists and cannot be read
- **THEN** `/locked` and `/locked/x.txt` answer 404

#### Scenario: A double star inside a name matches like a single star

- **WHEN** an ignore file holds `foo**bar`
- **THEN** `fooxbar` is ignored and `foo/x/bar` is not

#### Scenario: Patterns anchor, match wildcards and take other characters literally

- **WHEN** an ignore file holds a comment, a blank line, `*.log`, `/top.txt`, a `doc/*` pattern
  with an extension, `**/cache`, `build/**`, `a?c`, `[x].txt` and `deep/***/leaf`
- **THEN** `*` and `?` match within one name only, `?` matches one character of any width, and
  `**` matches across folders
- **AND** a pattern with a leading or inner `/` matches from the ignore file's folder only, while
  one without matches at any depth
- **AND** `[x].txt` matches only that literal name, and the comment matches nothing

#### Scenario: A directory-only pattern matches a folder not made yet

- **WHEN** an ignore file holds `build/` and a writer is about to make a folder named `build`
- **THEN** that new folder is ignored, at the root and in a subfolder
- **AND** a file named `build`, or a folder with another name, is not

### Requirement: Indistinguishable refusals

The system SHALL answer a path that is missing, hidden, ignored or not served in the same way: a
browser gets the same "Page not found" page with links home, no login or logout and no echo of
the path, under the same content security policy, and any other client gets a plain
`Not Found`.

#### Scenario: Browser and non-browser refusals do not distinguish unavailable paths

- **WHEN** a client asks for a missing file, a dot-file, a thumbnail of a text file, or the login,
  logout or dashboard paths with no login or write password set, by GET or POST
- **THEN** a browser gets a 404 page headed "Page not found" that links to `/`, shows the Guest
  label, offers no login or logout, and does not hold the path
- **AND** the page carries the app's content security policy
- **AND** a client that does not ask for HTML gets a 404 with the body `Not Found`
