# changelog

A simple tool to generate changelogs from Git logs.

## Usage

Run on the command-line:

```
$ changelog github.com/kevin-hanselman/changelog
# github.com/kevin-hanselman/changelog


#### `b457bb1` initial commit
```

Run as an HTTP server:

```
$ changelog -http :8888 &

$ curl localhost:8888/github.com/kevin-hanselman/changelog
# github.com/kevin-hanselman/changelog


#### `b457bb1` initial commit
```

Currently the output template is hardcoded and uses Markdown. The plan is to
support user templates and automatic Markdown-to-HTML conversion.
