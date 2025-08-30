# Go-Short-Link

Go‑Short‑Link is a tiny tool that lets you create short, memorable links for your team or website and send visitors to 
the right destination with a single click. 

You manage all links in a simple Google Sheets spreadsheet. Think of one 
column for the short path and one for the full URL. Anyone with access to the sheet can add, change, or remove 
entries without touching a server or writing code. The application regularly reads the sheet and updates itself automatically, 
keeping everything in sync. If the sheet is ever briefly unavailable, it can keep serving your last known links so 
nothing breaks. 

Because it’s designed to be lightweight, it runs fast, uses very little memory, and doesn’t pull in extra
services or heavy frameworks. In practice, that means you get a straightforward, easy‑to‑operate short‑link service: 
set up the sheet, configure your access to the Google API, point the application at it, and enjoy instant, 
low‑maintenance link management with no external dependencies to install or maintain.

If you are interested in using this tool, please check out the [admin guide](admin.md).

In case you want to contribute to the project, please check out the [development guide](develop.md).

```{toctree}
:maxdepth: 2
:caption: Docs
:hidden:
:numbered:

admin
develop
api

```

```{toctree}
:maxdepth: 1
:caption: Links
:hidden:

Repository <https://github.com/fanonwue/go-short-link>
Container Images <https://github.com/fanonwue/go-short-link/pkgs/container/go-short-link>

```