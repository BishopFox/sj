# sj (Swagger Jacker)

![](img/sj-logo.png)

sj is a command line tool designed to assist with auditing of exposed Swagger/OpenAPI definition files by checking the associated API endpoints for weak authentication. It also provides command templates for manual vulnerability testing.

It does this by parsing the definition file for paths, parameters, and accepted methods before using the results with one of five sub-commands:
- `automate` - Crafts a series of requests and analyzes the status code of the response.
- `prepare` - Generates a list of commands to use for manual testing.
- `endpoints` - Generates a list of raw API routes. *Path values will not be replaced with test data*.
- `brute` - Sends a series of requests to a target to find operation definitions based on commonly used file paths.
- `convert` - Converts a definition file from v2 to v3.

## Build

To compile from source, ensure you have Go version `>= 1.22.5` installed and run `go build` from within the repository:

```bash
$ git clone https://github.com/BishopFox/sj.git
$ cd sj/
$ go build .
```

## Install

To install the latest version of the tool, run:

```bash
$ go install github.com/BishopFox/sj@latest

# Note: you may also need to place the path to your Go binaries within your PATH environment variable:
$ export PATH=$PATH:~/go/bin
```

## Usage

> Use the `automate` command to send a series of requests to each defined endpoint and analyze the status code of each response.

```bash
$ sj automate -u https://petstore.swagger.io/v2/swagger.json -qi -p http://127.0.0.1:8080

INFO[0000] Gathering API details.                       
Title: Swagger Petstore
Description: This is a sample server Petstore server.  You can find out more about Swagger at [http://swagger.io](http://swagger.io) or on [irc.freenode.net, #swagger](http://swagger.io/irc/).  For this sample, you can use the api key `special-key` to test the authorization filters.
INFO Endpoint accessible!                          Method=GET Status=200 Target=/v2/v2/pet/findByTags
INFO Endpoint accessible!                          Method=GET Status=200 Target=/v2/v2/pet/1
WARN Manual testing may be required.               Method=POST Status=415 Target=/v2/v2/pet/1
WARN Manual testing may be required.               Method=POST Status=400 Target=/v2/v2/store/order
WARN Manual testing may be required.               Method=POST Status=400 Target=/v2/v2/user/createWithList
INFO Endpoint accessible!                          Method=GET Status=200 Target=/v2/v2/user/login
INFO Endpoint accessible!                          Method=GET Status=200 Target=/v2/v2/user/logout
WARN Manual testing may be required.               Method=POST Status=415 Target=/v2/v2/pet/1/uploadImage
WARN Manual testing may be required.               Method=POST Status=400 Target=/v2/v2/pet
WARN Manual testing may be required.               Method=PUT Status=415 Target=/v2/v2/pet
INFO Endpoint accessible!                          Method=GET Status=200 Target=/v2/v2/pet/findByStatus
INFO Endpoint accessible!                          Method=GET Status=200 Target=/v2/v2/store/inventory
INFO Endpoint accessible!                          Method=GET Status=200 Target=/v2/v2/store/order/1
ERRO Endpoint not found.                           Method=GET Status=404 Target=/v2/v2/user/bishopfox
WARN Manual testing may be required.               Method=PUT Status=415 Target=/v2/v2/user/bishopfox
WARN Manual testing may be required.               Method=POST Status=400 Target=/v2/v2/user/createWithArray
WARN Manual testing may be required.               Method=POST Status=400 Target=/v2/v2/user
```

### Bulk + Fallback Mode

The `automate` command supports bulk target processing and discovery fallback:

- `--url-file <path>`: newline-delimited targets (hosts or URLs). Blank lines and `# comments` are ignored.
- `--fallback-brute`: if direct spec loading fails, automatically run brute discovery and then automate each discovered spec.

When a host is listed without a scheme, `https://` is assumed.

**Examples:**

```bash
# Bulk automate from file
$ sj automate --url-file targets.txt -F json

# Single target with fallback discovery
$ sj automate -u https://example.com/wrong/path/swagger.json --fallback-brute
```

### Enhanced Interactive Mode

The `automate` command supports an `--enhanced` flag that enables an interactive mode designed for investigating ambiguous API responses. This mode is particularly useful for:
- LLM/MCP tool integration for automated API testing
- Manual debugging of endpoints that return unclear error messages
- Iterative refinement of requests when initial attempts fail

When enabled, the enhanced mode activates for responses that are ambiguous (4xx/5xx errors except 401/403/404). For successful responses (2xx) or clear auth errors (401/403), the tool auto-advances to the next endpoint.

**Example workflow:**

```bash
$ sj automate -l api-spec.yaml --enhanced

=== REQUEST (Attempt 1/5) ===
Method: POST
URL: https://api.example.com/v1/users
Query: limit=10
Headers: Content-Type=application/json
Body: {
  "name": "test",
  "email": "test@example.com"
}

=== RESPONSE ===
Status: 500 Internal Server Error
Body: {"error": "only one name must be first and last"}

[Modify request or N for next]: Body: {"name":"Aaron Ringo","email":"test@example.com"}

=== REQUEST (Attempt 2/5) ===
Method: POST
URL: https://api.example.com/v1/users
Query: limit=10
Headers: Content-Type=application/json
Body: {
  "name": "Aaron Ringo",
  "email": "test@example.com"
}

=== RESPONSE ===
Status: 201 Created
Body: {"id": 123, "name": "Aaron Ringo", "email": "test@example.com"}

=== AUTO-ADVANCING (Success) ===
```

**Supported modifications:**

- `Body: {...}` - Replace entire request body with JSON
- `Query: key=value` - Set or update a query parameter
- `Path: key=value` - Set or update a path parameter
- `Header: key=value` - Set or update a header
- `key=value` - Smart detection (automatically determines if it's a query, path, or body field)
- `key=` - Delete a parameter (empty value)
- `N` or `next` - Skip to next endpoint
- `Q` or `quit` - Exit the tool

**Spec validation:**

When modifications violate the OpenAPI specification (e.g., adding parameters not defined in the spec, wrong type), the tool displays warnings and prompts for confirmation:

```
==================================================
WARNING: Query parameter 'custom' not defined in spec
==================================================
Continue outside spec? [Y/n]: 
```

**Flags:**

- `--enhanced` - Enable interactive mode
- `--max-retries int` - Maximum attempts per endpoint (default: 5)
- `--url-file string` - Bulk target input file
- `--fallback-brute` - Run brute discovery when direct spec loading fails

**Flag conflicts:**

- `-u/--url` cannot be used with `--url-file`
- `--fallback-brute` cannot be used with `--local-file`

**Note:** Enhanced mode is incompatible with `--quiet` flag as it requires interactive input.

> Use the `prepare` command to prepare a list of commands for manual testing. Currently supports both `curl` and `sqlmap`. You will likely have to modify these slightly.

```bash
$ sj prepare -u https://petstore.swagger.io/v2/swagger.json -qi -p http://127.0.0.1:8080      

INFO[0000] Gathering API details.
                      
Title: Swagger Petstore
Description: This is a sample server Petstore server.  You can find out more about Swagger at [http://swagger.io](http://swagger.io) or on [irc.freenode.net, #swagger](http://swagger.io/irc/).  For this sample, you can use the api key `special-key` to test the authorization filters.
$ curl -X POST "https://petstore.swagger.io/v2/pet/{petId}"
$ curl -X GET "https://petstore.swagger.io/v2/pet/{petId}"
$ curl -X GET "https://petstore.swagger.io/v2/store/inventory"
$ curl -X POST "https://petstore.swagger.io/v2/user/createWithList" -d 'body=1'
$ curl -X GET "https://petstore.swagger.io/v2/user/logout"
$ curl -X POST "https://petstore.swagger.io/v2/user/createWithArray" -d 'body=1'
$ curl -X GET "https://petstore.swagger.io/v2/pet/findByStatus"
$ curl -X GET "https://petstore.swagger.io/v2/pet/findByTags"
$ curl -X POST "https://petstore.swagger.io/v2/store/order" -d 'petId=1&quantity=1&shipDate=bishopfox&status=bishopfox&complete=1&id=1&body='
$ curl -X POST "https://petstore.swagger.io/v2/pet/{petId}/uploadImage"
$ curl -X POST "https://petstore.swagger.io/v2/pet" -d 'photoUrls=1&tags=1&status=bishopfox&id=1&category=&name=doggie&body='
$ curl -X PUT "https://petstore.swagger.io/v2/pet" -d 'id=1&category=&name=doggie&photoUrls=1&tags=1&status=bishopfox&body='
$ curl -X GET "https://petstore.swagger.io/v2/user/{username}"
$ curl -X PUT "https://petstore.swagger.io/v2/user/{username}" -d 'email=bishopfox&password=bishopfox&phone=bishopfox&userStatus=1&id=1&username=bishopfox&firstName=bishopfox&lastName=bishopfox&body='
$ curl -X GET "https://petstore.swagger.io/v2/user/login"
$ curl -X POST "https://petstore.swagger.io/v2/user" -d 'phone=bishopfox&userStatus=1&id=1&username=bishopfox&firstName=bishopfox&lastName=bishopfox&email=bishopfox&password=bishopfox&body='
$ curl -X GET "https://petstore.swagger.io/v2/store/order/{orderId}"
```

> Use the `endpoints` command to generate a list of raw endpoints from the provided definition file.

```bash
$ sj endpoints -u https://petstore.swagger.io/v2/swagger.json -qi -p http://127.0.0.1:8080

INFO[0000] Gathering endpoints.
                        
/v2/store/inventory
/v2/store/order/{orderId}
/v2/pet
/v2/pet
/v2/store/order
/v2/user/createWithList
/v2/pet/{petId}/uploadImage
/v2/pet/findByTags
/v2/pet/{petId}
/v2/pet/{petId}
/v2/user/{username}
/v2/user/{username}
/v2/user/createWithArray
/v2/pet/findByStatus
/v2/user/login
/v2/user/logout
/v2/user
```

> Use the `brute` command to send a series of requests in an attempt to find a definition file on the target.

```bash
$ sj brute -u https://petstore.swagger.io -qi -p http://127.0.0.1:8080 -e
INFO[0000] Sending 2173 requests. This could take a while... 
Request: 343
INFO[0033] Definition file found: https://petstore.swagger.io/v2/swagger 
```

The `brute` command also supports multi-spec workflows:

- `--continue`: keep scanning after first discovered spec
- `--max-found <n>`: stop after `n` unique discovered specs (`0` = unlimited)
- `--run-automate`: immediately run `automate` checks for each discovered spec

**Examples:**

```bash
# Continue scanning for multiple specs (e.g., v1/v2/v3)
$ sj brute -u https://example.com --continue --max-found 10

# Discover and immediately test each discovered spec
$ sj brute -u https://example.com --continue --run-automate
```

**Flag conflicts:**

- `--max-found` requires `--continue`
- `--run-automate` cannot be used with `--endpoint-only`

> Use the `convert` command to convert a definition file from version 2 to version 3.

```bash
$ sj convert -u https://petstore.swagger.io/v2/swagger.json -qi -p http://127.0.0.1:8080 -o openapi.json

INFO[0000] Gathering API details.
                      
INFO[0000] Wrote file to /current/directory/openapi.json 
```

## Help

A full list of commands can be found by using the `--help` flag:

```bash
$ sj --help
The process of reviewing and testing exposed API definition files is often tedious and requires a large investment of time for a thorough review.

sj (swaggerjacker) is a CLI tool that can be used to perform an initial check of API endpoints identified through exposed Swagger/OpenAPI definition files. 
Once you determine what endpoints require authentication and which do not, you can use the "prepare" command to generate command templates for further (manual) testing.

Example usage:

Perform a quick check of endpoints which require authentication:
$ sj automate -u https://petstore.swagger.io/v2/swagger.json

Generate a list of commands to use for manual testing:
$ sj prepare -u https://petstore.swagger.io/v2/swagger.json

Generate a list of raw API routes for use with custom scripts:
$ sj endpoints -u https://petstore.swagger.io/v2/swagger.json

Perform a brute-force attack against the target to identify hidden definition files:
$ sj brute -u https://petstore.swagger.io

Convert a Swagger (v2) definition file to an OpenAPI (v3) definition file:
$ sj convert -u https://petstore.swagger.io/v2/swagger.json -o openapi.json

Usage:
  sj [flags]
  sj [command]

Available Commands:
  automate    Sends a series of automated requests to the discovered endpoints.
  brute       Sends a series of automated requests to discover hidden API operation definitions.
  convert     Converts a Swagger definition file to an OpenAPI v3 definition file.
  endpoints   Prints a list of endpoints from the target.
  help        Help about any command
  prepare     Prepares a set of commands for manual testing of each endpoint.

Flags:
  -A, --agent string            Set the User-Agent string. (default "Swagger Jacker (github.com/BishopFox/sj)")
  -b, --base-path string        Set the API base path if not defined in the definition file (i.e. /V2/).
  -f, --format string           Declare the format of the definition file (json/yaml/yml/js). (default "json")
  -H, --headers stringArray     Add custom headers, separated by a colon ("Name: Value"). Multiple flags are accepted.
  -h, --help                    help for sj
  -i, --insecure                Ignores server certificate validation.
  -l, --local-file string       Loads the documentation from a local file.
  -o, --outfile string          Output the results to a file. Only supported for the 'automate' and 'brute' commands at this time.
  -p, --proxy string            Proxy host and port. Example: http://127.0.0.1:8080 (default "NOPROXY")
  -q, --quiet                   Do not prompt for user input - uses default values for all requests.
      --randomize-user-agent    Randomizes the user agent string. Default is 'false'.
  -s, --safe-word stringArray   Avoids 'dangerous word' check for the specified word(s). Multiple flags are accepted.
  -T, --target string           Manually set a target for the requests to be made if separate from the host the documentation resides on.
  -t, --timeout int             Set the request timeout period. (default 30)
  -u, --url string              Loads the documentation file from a URL
  -v, --version                 version for sj

Use "sj [command] --help" for more information about a command.
```
