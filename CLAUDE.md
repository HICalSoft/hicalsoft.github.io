# CLAUDE.md — hicalsoft.github.io

## Site Overview

GitHub Pages site for **HICalSoft** (hicalsoft.com). Built with Jekyll using the Neumorphism dark theme. Includes standalone SPA pages for `/letters` and `/poetry`.

- **Domain**: hicalsoft.com (via CNAME)
- **Theme**: Neumorphism (dark mode, `#2b2d2f` background, `#fff` text)
- **Build**: Jekyll + Gulp pipeline (SCSS → CSS, JS minification, BrowserSync)
- **Fonts**: Raleway (body), Josefin Sans (numbers)

## Repository Structure

```
├── _config.yml                  # Jekyll site configuration
├── CNAME                        # Custom domain: hicalsoft.com
├── README.md                    # Theme README
├── letters/
│   ├── index.html               # Standalone SPA — love letters browser
│   └── data/
│       └── letters.json         # Combined letter data (~4.4 MB)
├── poetry/
│   ├── index.html               # Standalone SPA — poetry browser
│   └── data/
│       └── poetry.json          # Combined poetry data (~4.7 MB)
├── _assets/                     # Theme SCSS/JS source
├── _data/                       # Jekyll data files
├── _includes/                   # Jekyll includes
├── _layouts/                    # Jekyll layouts
├── _posts/                      # Blog posts
├── assets/                      # Compiled CSS/JS/images
├── gulpfile.js                  # Gulp build tasks
├── Gemfile                      # Ruby dependencies
└── package.json                 # Node dependencies
```

## Letters & Poetry Pages

Both `/letters` and `/poetry` are **standalone HTML pages** — they do NOT use Jekyll layouts or the Gulp build pipeline. They are self-contained SPAs that match the site's visual theme.

### Design System

| Property | Letters | Poetry |
|----------|---------|--------|
| Accent color | `#ff073a` (red) | `#c084fc` (purple) |
| Icon | ✉ | 📖 |
| Body font | Cormorant Garamond (serif) | Cormorant Garamond (serif) |
| UI font | Raleway (sans-serif) | Raleway (sans-serif) |

### Features

- **Sidebar**: Browse/filter by author or poet (with search)
- **Card grid**: Scrollable list with preview text
- **Detail view**: Full text with metadata and source attribution
- **Random button**: Shows a random letter/poem
- **Font controls**: Auto-fit sizing to prevent line wrapping on mobile
  - A+/A− buttons for manual adjustment (8–28px range, ±2px steps)
  - "auto" label (clickable to reset) calculates ideal size from container width and longest line
  - Recalculates on window resize when in auto mode
- **Keyboard navigation**: ← → arrows, Escape to go back, R for random
- **Mobile responsive**: Slide-out sidebar with overlay at ≤768px

### JSON Data Format

Both data files use the same structure:

```json
{
  "authors": { "Author Name": count, ... },
  "letters": [ { "a": "author", "r": "recipient", "h": "heading", "b": "body", "s": "source", "p": "period" }, ... ]
}
```

Poetry uses `"poems"` instead of `"letters"`, and `"t"` (title) instead of `"r"` and `"h"`:

```json
{
  "authors": { "Poet Name": count, ... },
  "poems": [ { "t": "title", "b": "body", "a": "author", "s": "source", "p": "period" }, ... ]
}
```

### Data Regeneration

The JSON data files are generated from the main `letters` repository using `generate_web_data.py`:

```bash
# From the letters/ project root:
python3 generate_web_data.py              # Regenerates both letters.json and poetry.json
python3 generate_web_data.py --letters    # Letters only
python3 generate_web_data.py --poetry     # Poetry only
```

This reads the individual JSON files from `letters/` and `poetry/` and writes combined data to `hicalsoft.github.io/*/data/`. See the main repo's CLAUDE.md for the full pipeline (downloading sources → parsing → generating web data).

## Change Log

### Add font size controls and update poetry data
- Added auto-fit font sizing to both /letters and /poetry detail views
- Calculates optimal font size based on container width and longest line (~0.52em char ratio)
- Added A+/A− manual controls and clickable "auto" reset label
- Recalculates on window resize
- Updated poetry.json with fixed Poe data (51 clean poems, down from 108 with junk)
- Fixed poetry.json structure: now uses `{authors, poems}` format (was flat array causing load failure)

### Add /poetry page
- Created standalone SPA at `/poetry/index.html` with purple accent theme
- Generated `poetry.json` (3,098 poems from 15 Gutenberg sources)
- Same feature set as /letters: sidebar, cards, detail, random, keyboard nav

### Add /letters page
- Created standalone SPA at `/letters/index.html` with red accent theme
- Generated `letters.json` (1,307 letters from 11 Gutenberg sources)
- Dark neumorphism design matching the main site
- Features: author sidebar, card grid, detail view, random button, mobile responsive
