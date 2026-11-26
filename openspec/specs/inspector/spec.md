# inspector Specification

## Purpose

Selecting a row of a listing opens an inspector beside it with a preview and the entry's facts.
This spec states what the inspector shows, and how selection behaves for any viewer.

## Requirements

### Requirement: Inspector

Clicking a row SHALL select it and open an inspector with a preview (the thumbnail, an SVG, a
video or audio player, or the kind's icon), the kind with the pixel size once the thumbnail has
been made, size, UTC modified time and folder, and Download (Open for a folder) and Copy link.
Selecting, deselecting and filtering SHALL be open to any viewer. They SHALL survive a relist, and
a selected entry that disappears SHALL close the inspector.

#### Scenario: A guest selects and filters; SVG previews as an image, HTML never embeds; no write controls

- **WHEN** a guest selects an SVG file, then an HTML file, then filters the rows
- **THEN** the SVG previews as an image of its own URL
- **AND** the HTML file shows in no frame, object, embed or image
- **AND** the inspector offers no rename, delete or move, and the filter applies

#### Scenario: Only the last selected row is marked, and a row the filter hid stays hidden

- **WHEN** a guest selects one row and then another, or selects a row the filter keeps after
  another row was selected and filtered away
- **THEN** only the last selected row is marked selected, and the filtered-away row stays hidden

#### Scenario: An image's kind line carries its pixel size once its thumbnail is made

- **WHEN** a guest selects a PNG image before and after its thumbnail is made
- **THEN** the kind line reads "PNG image" first, and then "PNG image · 200 × 100"

#### Scenario: A JPEG turned a quarter by its EXIF orientation shows its size as displayed

- **WHEN** a guest selects a 200 by 100 JPEG whose EXIF orientation turns it a quarter, after its
  thumbnail is made
- **THEN** the kind line gives its size as displayed, "100 × 200"

#### Scenario: The selection clears when its file is removed outside the page

- **WHEN** a guest selects a file and the file is then removed from the disk
- **THEN** the inspector closes within a second

#### Scenario: A selected folder offers Open, and a file Download

- **WHEN** a guest selects a folder, and then a file
- **THEN** the folder's inspector offers Open and Copy link, and the file's offers Download and
  Copy link
