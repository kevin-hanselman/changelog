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
$ changelog -serve :8888
```

Currently the output template is hardcoded and uses Markdown. The plan is to
support user templates and automatic Markdown-to-HTML conversion.
