# writes Specification

## Purpose

With a write password set, a user who logs in with it can change the served folder from the
listing. This spec states how write rights are granted and lost, which writes there are, and what
a write is never allowed to do.

## Requirements

### Requirement: Write login

With `SLIMSERVE_WRITE_PASSWORD` set, `/.app/login` SHALL also take the write password, under the
same rate limit. With viewer login on the username MUST match too. With it off the form SHALL ask
only for the password and every listing's header SHALL link to it. A write login SHALL hold until
logout, and changing or removing the write password SHALL end it, on open pages at their next
write too.

#### Scenario: With login off, the login page asks only for the write password

- **WHEN** a browser loads the login page with a write password set and viewer login off
- **THEN** the form has a password field and no username field

#### Scenario: With login off, the listing links to the login page and shows no write UI until the user logs in

- **WHEN** a guest loads a listing with a write password set
- **THEN** its header links to the login page, and it shows no write controls

#### Scenario: Logging in with the write password shows the write UI and a logout button

- **WHEN** a guest logs in with the write password
- **THEN** the listing shows the write controls and a logout button, and no login link

#### Scenario: A wrong write password answers 401 and grants no writes

- **WHEN** a guest logs in with a wrong password
- **THEN** the answer is 401 and the listing shows no write controls

#### Scenario: The 11th wrong write login from one address answers 429, even with the right password

- **WHEN** a client fails to log in 10 times and then sends the write password
- **THEN** the answer is 429

#### Scenario: Changing the write password revokes the write session

- **WHEN** the write password changes after a browser logged in with it
- **THEN** its next page shows no write controls, and a delete sent from it changes nothing

#### Scenario: Removing the write password revokes the write session

- **WHEN** the write password is removed after a browser logged in with it
- **THEN** its next page shows no write controls

#### Scenario: A forged write session grants no writes

- **WHEN** a browser's session claims write rights the server did not grant
- **THEN** its page shows no write controls, and a delete sent from it changes nothing

#### Scenario: An open write page stops writing once the write password changes

- **WHEN** a page opened with write rights sends any write after the write password changed
- **THEN** the disk is unchanged

#### Scenario: A write session stops writing once the write password is unset

- **WHEN** a browser that logged in with the write password opens a page after the write password
  was unset, and sends any event
- **THEN** the page shows no write controls and the disk is unchanged

#### Scenario: Logging out tells that browser's open write pages to disconnect, and they remount without writes

- **WHEN** a browser logged in with the write password logs out
- **THEN** its open pages are told to disconnect
- **AND** a page it opens next has no write controls

#### Scenario: After a second write login in the same browser, logging out still reaches the first login's pages

- **WHEN** a browser logs in with the write password twice and then logs out
- **THEN** the pages it opened after the first login are told to disconnect

#### Scenario: The write password with the right username logs in with writes

- **WHEN** viewer login is on and a browser logs in with the username and the write password
- **THEN** it is logged in and its listing shows the write controls

#### Scenario: The write password with a wrong username is refused

- **WHEN** viewer login is on and a browser sends the write password with a wrong username
- **THEN** the answer is 401 and the browser is still sent to the login page

#### Scenario: The viewer password logs in without writes

- **WHEN** viewer login and a write password are on and a browser logs in with the viewer password
- **THEN** its listing shows no write controls

#### Scenario: A viewer login after a write login in the same browser drops the writes

- **WHEN** a browser logged in with the write password logs in again with the viewer password
- **THEN** its listing shows no write controls

### Requirement: Write settings

`SLIMSERVE_WRITE_PASSWORD` SHALL set the write password, and an empty value SHALL count as unset.
A server SHALL refuse to boot when the write password equals the viewer password.

#### Scenario: SLIMSERVE_WRITE_PASSWORD sets the write password, and an empty one counts as unset

- **WHEN** a server starts with `SLIMSERVE_WRITE_PASSWORD` set, set empty, or unset
- **THEN** writes take that password, or there is no write password

#### Scenario: A write password equal to the viewer password refuses to boot

- **WHEN** a server starts with login on and the write password equal to the viewer password
- **THEN** it refuses to boot, naming `SLIMSERVE_WRITE_PASSWORD`
- **AND** a different write password boots

### Requirement: Writes

With write rights, a listing SHALL upload files, create a folder, rename or move an entry to a
path from the root, and delete a file, a link (never its target) or a folder with everything in
it. A folder that holds files SHALL go only after a confirm bar that counts them, which asks again
when the folder or its count changed. Other open pages SHALL update through the live listing.
Without write rights every write event SHALL be refused.

#### Scenario: A writer uploads, makes a folder, moves a file there and back and deletes it, and another page follows

- **WHEN** a writer uploads a file, makes a folder, moves the file into it, moves it back and
  deletes it
- **THEN** the disk holds each result in turn
- **AND** a second open page lists each result within a second

#### Scenario: After opening a subfolder in the page, a new folder and an upload land in it

- **WHEN** a writer opens a subfolder in the page and then makes a folder and uploads a file
- **THEN** both land in the subfolder and not in the root

#### Scenario: An upload queued in one folder is dropped when the page opens another, so it lands in neither

- **WHEN** a writer queues an upload, opens another folder in the page, and submits
- **THEN** the file lands in neither folder

#### Scenario: A delete sent for the root's file after the page opened /sub deletes the root's file, not sub's

- **WHEN** a delete for a root file reaches the server after the page opened a subfolder holding a
  file of the same name
- **THEN** the root's file goes and the subfolder's stays

#### Scenario: A move sent for the root's file after the page opened /sub moves the root's file, not sub's

- **WHEN** a move for a root file reaches the server after the page opened a subfolder holding a
  file of the same name
- **THEN** the root's file moves and the subfolder's stays

#### Scenario: A successful upload leaves no temporary file

- **WHEN** a writer uploads a file
- **THEN** the folder holds the file and nothing else new

#### Scenario: A link is renamed within its own folder, and not moved to another

- **WHEN** a writer moves a relative link to another folder, and then renames it in its own
- **THEN** the move changes nothing, and the rename keeps the link's target

#### Scenario: A folder moves with its files, and a dot-file inside it does not block the move

- **WHEN** a writer moves a folder that holds files, a subfolder and a dot-file
- **THEN** the folder and all of its content, the dot-file included, are at the new path

#### Scenario: Deleting a link to a file or a folder removes the link and leaves its target

- **WHEN** a writer deletes a link to a folder and a link to a file
- **THEN** both links are gone and their targets stay whole

#### Scenario: Deleting an empty folder removes it at once

- **WHEN** a writer deletes an empty folder
- **THEN** it is gone with no confirm bar

#### Scenario: A folder with files is deleted only after the confirm bar that counts them

- **WHEN** a writer deletes a folder that holds two files, one of them a dot-file in a subfolder
- **THEN** a confirm bar counts 2 files and the folder stays
- **AND** cancel keeps the folder, and confirming deletes it

#### Scenario: A folder that gained a file after the confirm bar opened stays, and the bar counts again

- **WHEN** a file is added to a folder after its confirm bar opened, and the writer confirms
- **THEN** the folder stays and the bar asks again with the new count

#### Scenario: A folder replaced by another of the same count after the confirm bar opened stays

- **WHEN** a folder is replaced by another with the same count after its confirm bar opened, and
  the writer confirms
- **THEN** the new folder stays

#### Scenario: Deleting a folder that links out of the root leaves the outside folder whole

- **WHEN** a writer deletes a folder that holds a link to a folder outside the root
- **THEN** the folder is gone and the outside folder keeps its files

#### Scenario: Without write rights every event leaves the disk unchanged, and the write UI is absent

- **WHEN** a guest sends every write event, each on a page of its own
- **THEN** the disk is unchanged and the page shows no write controls

#### Scenario: A rename keeps the entry in its folder and the inspector follows it; a name with a slash is refused

- **WHEN** a writer renames a selected file from the inspector, then tries names with a slash or a
  leading dot, then renames a folder
- **THEN** the file is renamed in place and the inspector follows it
- **AND** the slash and dot names change nothing, and the folder is renamed

#### Scenario: Move to… moves the file into the folder picked in the dialog

- **WHEN** a writer opens Move to… on a file, unfolds a folder in the picker, picks a subfolder
  and submits
- **THEN** the file is in the picked folder and no longer where it was

#### Scenario: Move to… opens on the current folders each time, with nothing expanded from before

- **WHEN** a writer opens Move to… again after unfolding a folder in it and a new folder appeared
- **THEN** the picker opens folded, and unfolding shows the new folder

#### Scenario: A rename or move onto the entry's own place changes nothing and reports nothing

- **WHEN** a writer renames a file to its own name, moves it to its own folder, or drops it on its
  own folder's tree node
- **THEN** the disk is unchanged and no error shows
- **AND** the picker's submit is disabled, and the open folder's tree node takes no drop while
  others do

#### Scenario: Opening a row's menu and deleting from it leaves the selection where it was

- **WHEN** a writer with one row selected deletes another row from its ⋯ menu
- **THEN** that row's file is gone, and the selection and inspector stay on the first row

#### Scenario: Move to… from a row's menu opens the dialog on that row and leaves the selection and the inspector

- **WHEN** a writer with one row selected opens Move to… from another row's menu and moves it
- **THEN** the selection and inspector stay on the first row, and only the other row's file moves

### Requirement: Write shortcuts

With write rights, a row dropped on a sidebar folder SHALL move there. With an entry selected, F2
SHALL focus the inspector's name field, M SHALL open the folder picker, and the Delete key SHALL
delete the selection as its Delete button does: a file at once, and a folder with entries after
the confirm bar.

#### Scenario: Keys rename and move the selection

- **WHEN** a writer presses F2 or M with an entry selected
- **THEN** F2 focuses the inspector's name field, and M opens the folder picker

### Requirement: Upload settings

Uploads SHALL be at most `SLIMSERVE_UPLOAD_MAX_MB`, default 100, and only of the
`SLIMSERVE_UPLOAD_TYPES` extensions when it is set, judged by the file's name in any case. A
rename SHALL keep a file on that list. A server SHALL refuse to boot when the list names no
extension, or one with no known MIME type.

#### Scenario: Uploads default to 100 MB of any type

- **WHEN** a server starts with neither upload variable set, and then with `SLIMSERVE_UPLOAD_MAX_MB=5`
- **THEN** uploads take up to 100 MB of any type, and then up to 5 MB

#### Scenario: SLIMSERVE_UPLOAD_TYPES takes extensions with or without their leading dot

- **WHEN** `SLIMSERVE_UPLOAD_TYPES` holds `txt`, or `txt, .png`
- **THEN** uploads take those extensions

#### Scenario: SLIMSERVE_UPLOAD_TYPES entries match in any case

- **WHEN** `SLIMSERVE_UPLOAD_TYPES` holds `JPG`
- **THEN** uploads take the jpg extension

#### Scenario: An SLIMSERVE_UPLOAD_TYPES that lists no extension refuses to boot

- **WHEN** a server starts with `SLIMSERVE_UPLOAD_TYPES` set to a lone comma
- **THEN** it refuses to boot, naming the variable

#### Scenario: An upload type with no known MIME type refuses to boot

- **WHEN** `SLIMSERVE_UPLOAD_TYPES` lists an extension with no known MIME type
- **THEN** the server refuses to boot, naming the variable

#### Scenario: An upload over the size limit or of a type off the list is refused, with the entry's error shown

- **WHEN** a writer uploads a file over the size limit, or one whose extension is off the list
- **THEN** the upload is refused with an error on its entry, and the disk is unchanged

#### Scenario: An upload named off the type list is refused, whatever type the browser claims

- **WHEN** only jpg is allowed and a writer uploads an HTML file the browser labels a JPEG
- **THEN** the upload is refused and the disk is unchanged

#### Scenario: A file renamed to an extension off the type list is refused

- **WHEN** only jpg is allowed and a writer renames a jpg file to an HTML name, and then to an
  upper-case JPG name
- **THEN** the first rename changes nothing, and the second renames the file

### Requirement: Write refusals

A write SHALL be refused when it names a dot-file, a name with a "/", an empty or non-UTF-8 name,
an ignored name by its requested or resolved path, a path outside the root or under a hidden or
ignored folder, or an existing name, so nothing is ever overwritten. A folder whose subtree holds
an entry an ignore pattern hides at its current path SHALL not move, and a folder SHALL not move
into itself.

#### Scenario: A write path that climbs out, hides a slash, or names a hidden or ignored entry is refused

- **WHEN** a writer sends a delete, a move, a rename and a new folder for paths that climb out of
  the root, hide a slash or `..` in an escape, or name a dot-file, a dot-folder or an ignored entry
- **THEN** the root and the folder outside it are unchanged

#### Scenario: A folder renamed onto an existing empty folder is refused, and both stay

- **WHEN** a writer renames a folder to the name of an existing empty folder
- **THEN** both folders stay as they were

#### Scenario: An upload onto an existing name is refused, leaving its bytes and no temporary file

- **WHEN** a writer uploads a file whose name exists
- **THEN** the existing file keeps its bytes and the folder holds nothing new

#### Scenario: A rename or a new folder onto an existing name is refused, and both stay

- **WHEN** a writer renames a file to an existing file's or folder's name, or makes a folder with
  an existing file's name
- **THEN** the disk is unchanged

#### Scenario: A rename out of the root, to a dot-name or of a folder into its own child is refused

- **WHEN** a writer renames a file to a path above the root, to a dot-name, to a dot-name in a
  subfolder or to an empty name, or moves a folder into its own child
- **THEN** the disk is unchanged

#### Scenario: A write aimed at a hidden or ignored name, or into an ignored folder, is refused

- **WHEN** a writer makes a folder, renames, moves, deletes or uploads with an ignored name, into an
  ignored folder, or on a dot-file
- **THEN** the disk is unchanged

#### Scenario: A folder holding a file an ignore pattern hides at its current path does not move

- **WHEN** a writer renames a folder that holds a file an anchored ignore pattern hides
- **THEN** the folder does not move, so the file is not revealed at a new path

#### Scenario: A new name in the root or in a folder is written at that name

- **WHEN** a write names a new visible name in the root or in a subfolder
- **THEN** it lands at exactly that name

#### Scenario: A name that is empty, a dot-name, holds a slash or is not UTF-8 is refused

- **WHEN** a write names an empty name, `.`, `..`, a dot-name, a name with a slash or one that is
  not valid UTF-8, in the root or a subfolder
- **THEN** it is refused, so a delete can never name the folder being listed

#### Scenario: An ignored name is refused, and a dir-only pattern refuses only a folder

- **WHEN** an ignore file names `secret.txt` and `build/`
- **THEN** a write named `secret.txt` is refused in the root and a subfolder
- **AND** a new folder named `build` is refused, and a file named `build` is not

#### Scenario: A name ignored only at the path it resolves to, through a parent link, is refused

- **WHEN** a folder link points at a folder whose name an anchored pattern ignores below it
- **THEN** a write of that name through the link or in the real folder is refused, and another
  name through the link lands in the real folder

#### Scenario: A name ignored only at its requested path, through a parent link, is refused there only

- **WHEN** an anchored pattern ignores a name below a folder link, but not below its target
- **THEN** a write of that name through the link is refused, and in the real folder it is not

#### Scenario: A root-level name kept by a negation under a catch-all ignore file is allowed

- **WHEN** the root's ignore file holds `*` and then `!keep.txt`
- **THEN** a write named `keep.txt` is allowed, and any other name is refused

#### Scenario: A parent that is a dot-dir, ignored, missing, a file or a link out of the root is refused

- **WHEN** a write names a new entry under a dot-folder, an ignored folder, a missing folder, a
  file, or a link that leaves the root
- **THEN** it is refused

#### Scenario: A write to an existing link acts on the link, never its target

- **WHEN** a write names an existing link to a folder or a file
- **THEN** it acts on the link itself

#### Scenario: Uploads and file moves need hard links

- **WHEN** the served folder is on a file system without hard links, such as exFAT
- **THEN** uploads and file moves are refused, and nothing is overwritten

### Requirement: Dashboard

`/.app/dashboard`, the operations dashboard, SHALL answer 404 with no write password, a write
session's browser only, else a 302 to the login page or a 401. Its live socket SHALL be refused at
mount without write rights.

#### Scenario: The dashboard answers 404 with no write password, or an empty one

- **WHEN** a browser asks for `/.app/dashboard` with the write password unset or empty
- **THEN** the answer is 404

#### Scenario: The dashboard sends a browser without write rights to the login page, and answers anything else 401

- **WHEN** a write password is set and a browser with no login, or with a viewer login, asks for
  the dashboard
- **THEN** the browser is sent to the login page, and a client that is not a browser gets 401

#### Scenario: The dashboard answers a write session, and refuses its live socket once the password changed

- **WHEN** a browser logged in with the write password loads the dashboard, and the write password
  changes before it connects
- **THEN** the page answers 200, and its live socket is sent to the login page

#### Scenario: Changing the viewer password refuses the dashboard's live socket of a write session

- **WHEN** a write session under viewer login loads the dashboard, and the viewer password changes
  before it connects
- **THEN** its live socket is sent to the login page
