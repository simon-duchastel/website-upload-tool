# Website Upload Tool

This tool

## Building

Make sure you have [go](https://go.dev) and [make](https://www.gnu.org/software/make/manual/make.html) installed.

You can build the tool by running `make`. That's it!

## Running the tool

Run the tool by running `./website [command]`. For a more comprehensive guide on all commands, run `./website help`.

A few of the most useful commands are below:

- `preview`: builds the website and previews it locally at http://localhost:1313.
- `deploy`: builds the website and uploads it to the webhost. Requires subdomain flag, ex. `subdomain simon`.
- `rollback`: if there's a mistake in the `deploy` command, you can run this command with the same subdomain flag as before to rollback the website on the webhost to what was there before.