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
go run . automate -u https://petstore.swagger.io/v2/swagger.json -qi -p http://127.0.0.1:8080               

Gathering API details.
Title: Swagger Petstore
Description: This is a sample server Petstore server.  You can find out more about Swagger at [http://swagger.io](http://swagger.io) or on [irc.freenode.net, #swagger](http://swagger.io/irc/).  For this sample, you can use the api key `special-key` to test the authorization filters.
✓  GET  200  /v2/pet/findByStatus
✓  GET  200  /v2/user/logout
⚠  POST  400  /v2/user/createWithArray
⚠  POST  400  /v2/store/order
✗  GET  404  /v2/store/order/1
⚠  POST  400  /v2/pet
⚠  PUT  415  /v2/pet
⚠  POST  400  /v2/user/createWithList
✗  GET  404  /v2/user/bishopfox
⚠  PUT  415  /v2/user/bishopfox
⚠  POST  400  /v2/user
⚠  POST  415  /v2/pet/1/uploadImage
✓  GET  200  /v2/pet/findByTags
✗  GET  404  /v2/pet/1
⚠  POST  415  /v2/pet/1
✓  GET  200  /v2/store/inventory
✓  GET  200  /v2/user/login
```

You can also request verbose output to see the partial (or full) response:

```bash
$ sj automate -u https://petstore.swagger.io/v2/swagger.json -qi -p http://127.0.0.1:8080 -v           

Gathering API details.
Title: Swagger Petstore
Description: This is a sample server Petstore server.  You can find out more about Swagger at [http://swagger.io](http://swagger.io) or on [irc.freenode.net, #swagger](http://swagger.io/irc/).  For this sample, you can use the api key `special-key` to test the authorization filters.
✗  GET  404  /v2/user/bishopfox
   {"code":1,"type":"error","message":"User not found
⚠  PUT  415  /v2/user/bishopfox
   {"code":415,"type":"unknown","message":"com.sun.je
✓  GET  200  /v2/user/logout
   {"code":200,"type":"unknown","message":"ok"}
⚠  POST  400  /v2/user/createWithArray
   {"code":400,"type":"unknown","message":"bad input"
⚠  POST  400  /v2/user/createWithList
   {"code":400,"type":"unknown","message":"bad input"
✗  GET  404  /v2/pet/1
   {"code":1,"type":"error","message":"Pet not found"
⚠  POST  415  /v2/pet/1
   {"code":415,"type":"unknown"}
✓  GET  200  /v2/store/inventory
   {"sold":117,"string":26,"invalidStatus":1,"-1":1,"
⚠  POST  400  /v2/store/order
   {"code":400,"type":"unknown","message":"bad input"
✓  GET  200  /v2/user/login
   {"code":200,"type":"unknown","message":"logged in 
⚠  POST  400  /v2/pet
   {"code":400,"type":"unknown","message":"bad input"
⚠  PUT  415  /v2/pet
   {"code":415,"type":"unknown","message":"com.sun.je
✓  GET  200  /v2/pet/findByStatus
   []
✓  GET  200  /v2/pet/findByTags
   []
✗  GET  404  /v2/store/order/1
   {"code":1,"type":"error","message":"Order not foun
⚠  POST  400  /v2/user
   {"code":400,"type":"unknown","message":"bad input"
⚠  POST  415  /v2/pet/1/uploadImage
   {"code":415,"type":"unknown"}
```

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
