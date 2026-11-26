# browsing Specification

## Purpose

A GET on a folder's URL shows a listing page of what the server serves there. This spec states
which entries a listing holds and in what order, its two views, the page around it, and moving
between folders without a reload.

## Requirements

### Requirement: Served root

A server SHALL serve the directory `SLIMSERVE_ROOT` names. A production server started with
`PHX_SERVER` SHALL refuse to boot when it is unset or names something other than an existing
directory. A task that starts no server MUST load its configuration without a root.

#### Scenario: A server boots only when SLIMSERVE_ROOT names an existing directory

- **WHEN** a production server starts with `PHX_SERVER` and `SLIMSERVE_ROOT` naming an existing
  directory
- **THEN** it serves that directory
- **AND** with `SLIMSERVE_ROOT` unset, or naming a missing path or a file, it refuses to boot

#### Scenario: A task that starts no server loads without a root

- **WHEN** the configuration loads with `PHX_SERVER` and `SLIMSERVE_ROOT` unset
- **THEN** it loads, with the working directory as the root

### Requirement: Directory listing

A listing SHALL show exactly the entries the server would serve whose names are valid UTF-8,
folders first and then by name, under the app's content security policy.

#### Scenario: The listing links exactly the entries a GET serves, folders first

- **WHEN** a directory holds folders, files, a dot-file, an ignored file, a symlink to a file
  inside the root and a symlink to a file outside it
- **THEN** its listing links the folders and then the files, each group in name order
- **AND** the linked entries are exactly the entries whose GET answers 200

#### Scenario: A listed name with reserved characters downloads through its link

- **WHEN** a directory holds files named `a #1%.txt` and `é.txt`
- **THEN** following each link of its listing answers 200 with that file's bytes

#### Scenario: A fifo is neither served nor listed

- **WHEN** the root holds a named pipe
- **THEN** a GET for it answers 404
- **AND** the root's listing does not link it

#### Scenario: The listing carries the app CSP without the dashboard's nonce

- **WHEN** a client requests a listing
- **THEN** its content security policy holds `base-uri 'self'` and `frame-ancestors 'self'`
- **AND** it holds no script nonce

### Requirement: Listing views

A listing SHALL show as a list with sizes and modified times, the default, or as a grid of
thumbnails and kind icons. `?view=grid` or `?view=list` on a folder SHALL set the `slimserve-view`
cookie for a year and redirect to the bare path, so the choice survives a reload with no
JavaScript. Any other value SHALL be ignored.

#### Scenario: The Grid link keeps grid in a cookie for a year and returns to the folder

- **WHEN** a browser follows the Grid link of a folder, one whose name needs escaping included
- **THEN** the answer is a 302 back to that folder's path
- **AND** it sets `slimserve-view` to grid for at least a year

#### Scenario: After the Grid link, a reload renders the grid view

- **WHEN** a browser follows the Grid link and then loads the folder again
- **THEN** the folder renders the grid view, with Grid marked current

#### Scenario: From the grid, the List link keeps list in the cookie and a reload renders the list view

- **WHEN** a browser that shows the grid follows the List link and loads the folder again
- **THEN** the answer is a 302 back to the folder, setting `slimserve-view` to list
- **AND** the folder renders the list view, with List marked current

#### Scenario: With no cookie the folder renders the list view, and a bogus view sets none

- **WHEN** a browser with no view cookie loads a folder, and then loads it with `?view=bogus`
- **THEN** both render the list view
- **AND** the second sets no view cookie

#### Scenario: A tab left open from before the view switch reconnects to the list view

- **WHEN** a tab opened before the List/Grid switch existed reconnects to the server
- **THEN** it shows the list view, with List marked current and Grid not

#### Scenario: A mixed folder shows each entry's thumbnail or kind icon, its size and its modified time

- **WHEN** a folder holds a subfolder, a text file and a PNG image
- **THEN** the subfolder shows a folder icon and its count of visible entries, such as "2 items"
- **AND** the text file shows a document icon and its size in bytes, and the image its thumbnail
- **AND** each shows its modified time relative to now, such as "5m ago", "yesterday" or a date,
  with the UTC time to the minute on hover

### Requirement: Listing page

The listing page SHALL hold a sidebar with the folder tree, an "In this folder" card and the
session label with log in or log out, beside a panel with the crumbs, the count and total size, a
name filter, the List/Grid switch and the list. The list SHALL have Name, Kind, Size and Modified
columns, the time relative with the UTC time on hover, and a ⋯ menu per row.

#### Scenario: The folder tree shows each ancestor's folders with exact counts and the open path expanded

- **WHEN** a browser opens `/a/b`
- **THEN** the tree shows the root's folders, and the folders of `a` and of `a/b`, each with its
  count of visible entries
- **AND** `a` and `a/b` are expanded, `a/b` is marked current, and the others are folded
- **AND** a folder made in `a/b` later appears in the tree

#### Scenario: A guest unfolds a folder off the open path in place, and it stays unfolded across a relist and an open

- **WHEN** a guest unfolds a folder that is not on the open path
- **THEN** its subfolders appear with their counts, and the listing and title stay on the open folder
- **AND** it stays unfolded after a change relists the page and after the page opens another folder

#### Scenario: An unfolded level drops a deleted subfolder and shows a renamed one, with no click

- **WHEN** a folder is unfolded in the tree and one of its subfolders is deleted and another renamed
- **THEN** the tree drops the deleted one and shows the new name, with the folder's count updated

#### Scenario: A file added to a folder shown as a node updates that node's count

- **WHEN** a file is written into a folder the tree shows
- **THEN** that folder's count goes up within a second

#### Scenario: After an open away from a level, only changes in the levels the page still shows reach it

- **WHEN** a page on `/z/y` opens `/sub`
- **THEN** the page receives changes in exactly the root, `sub` and `z`, the levels it still shows
- **AND** changes in `z/y` and `z/y/x` no longer reach it

#### Scenario: An unfolded folder keeps the levels above it across an open, unfolded and live

- **WHEN** a page unfolds a folder two levels down and then opens another folder
- **THEN** the unfolded folder, its parent and its subfolders stay shown, and the page still
  receives changes in each of them

#### Scenario: A symlinked folder's node follows the link to its new target

- **WHEN** a link to one folder is pointed at another, and a file is then added to the new target
- **THEN** the link's node shows the new target's count, and then that count plus one

#### Scenario: Unfolding a hidden, ignored, .. or file path is refused, the tree unchanged

- **WHEN** a page asks to unfold a file, a missing path, a dot-folder, an ignored folder, a `..`
  path, an encoded `..` or slash, or an invalid escape
- **THEN** the request is refused and the page is unchanged

#### Scenario: The first paint, before the page connects, already lists the folder tree

- **WHEN** a browser loads a folder
- **THEN** the page as first sent already lists the tree's folders

#### Scenario: The folder card counts the open folder's own files by kind, by extension in any case

- **WHEN** a folder holds images with lower- and upper-case extensions, a text file, a dot-file,
  an ignored file and a subfolder with an image in it
- **THEN** the card counts and sizes only the folder's own visible files, by kind

#### Scenario: The name filter survives a relist, and a hidden file that changes stays hidden

- **WHEN** a page filters its rows by name and files change in its folder
- **THEN** a new file that matches shows, and a changed file the filter hides stays hidden

#### Scenario: The row menu offers reads to everyone and writes only with write rights

- **WHEN** a guest and a writer each open a row's ⋯ menu
- **THEN** both get Download and Copy link
- **AND** only the writer gets Rename, Move to… and Delete

#### Scenario: The page's icon link downloads the app's own favicon

- **WHEN** a browser follows the page's icon link
- **THEN** it gets the app's own favicon

#### Scenario: Keys fold the sidebar and focus the filter

- **WHEN** a user presses `[` on a listing page, or `/`
- **THEN** `[` folds or unfolds the sidebar, and `/` focuses the name filter

### Requirement: Sidebar rail

The sidebar SHALL fold to an icon rail by its button or `[`, and the `slimserve-rail` cookie SHALL
keep that across reloads. At phone width the sidebar SHALL be folded by default, and the open
sidebar SHALL be a drawer.

#### Scenario: The rail cookie renders the rail folded on the first paint

- **WHEN** a browser loads a folder with `slimserve-rail` set to rail, set to open, or unset
- **THEN** the sidebar is folded to the rail, open, or left to fold by screen width, in that order

### Requirement: In-page navigation

A listing page SHALL open another visible folder without a reload, and SHALL refuse a path that
is not a visible, listable folder and leave the page unchanged.

#### Scenario: A guest opens a subfolder: the list, crumbs, tree, title and selection follow, and its changes show

- **WHEN** a guest with a selection and a filter opens a subfolder in the page
- **THEN** the list, crumbs, tree, page title and path follow the subfolder, and the selection
  and inspector clear
- **AND** a file added to the subfolder then shows

#### Scenario: Opening a file, a missing, hidden or ignored folder, a .. path or an encoded trick is refused, the page unchanged

- **WHEN** a page asks to open a file, a missing path, a dot-folder, an ignored folder, a `..`
  path, an encoded `..` or slash, or an invalid escape
- **THEN** the request is refused and the page is unchanged

#### Scenario: Opening a path that is not a string is refused, and the page lives on

- **WHEN** a page asks to open a path that is a number
- **THEN** the request is refused and the page still shows its folder

#### Scenario: Opening a folder that resolves but cannot be listed is refused, the page unchanged

- **WHEN** a page asks to open a folder the server may enter but not read
- **THEN** the request is refused and the page is unchanged

#### Scenario: After opening a subfolder, a change in the folder it left no longer relists the page

- **WHEN** a page opens a subfolder, and then the folder it left changes
- **THEN** the page does not list again
