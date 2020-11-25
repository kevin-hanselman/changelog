# changelog

A simple tool to generate changelogs from Git logs.

## Usage

Run on the command-line:

```
$ changelog https://github.com/kevin-hanselman/changelog
# github.com/kevin-hanselman/changelog


#### `b457bb1` initial commit
```

Run as an HTTP server:

```
$ changelog -http :8888 &

$ curl localhost:8888/https/github.com/kevin-hanselman/changelog
# github.com/kevin-hanselman/changelog


#### `b457bb1` initial commit
```
