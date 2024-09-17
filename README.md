# Website Upload Tool

This tool is used to build and upload websites built with [hugo](https://gohugo.io). At the moment it's fairly bespoke for managing the sites in the duchastel.com domain.

**Note:** This tool is intended to be used within a directory containing a hugo site.

**Future work:** add the capability to do SSL cert rotation in this tool.

## Building

Run `make` to build. That's it!

Make sure you have [go](https://go.dev), [hugo](https://gohugo.io) and [make](https://www.gnu.org/software/make/manual/make.html) installed.

## Running the tool

Run the tool by running `./website (-sudomain [domain]) [command]`. For a more comprehensive guide on all commands, run `./website help`.

A few of the most useful commands are below:

- `preview`: builds the website and previews it locally at http://localhost:1313.
- `deploy`: builds the website and uploads it to the webhost. Requires subdomain flag, ex. `./website -subdomain simon deploy`.
- `rollback`: if there's a mistake in the `deploy` command, you can run this command with the same subdomain flag as before to rollback the website on the webhost to what was there before.

## Flags

- `-dry-run`: Print verbose debugging of all actions being taken, but don't execute anything (ex. don't upload files). Useful for testing.
