# README

## About


## How to run

- The cameras are only accessible via Browsers, inorder to run access camera browsers requires `https` based endpoints. You can generate a self-signed Openssl certificate and use just paste in the project directory.

```

openssl commands...

```

- After the running the above commands the project to should look something like this

```

> tree -L 1 .
.
├── README.md
├── app.go
├── build
├── cert.pem
├── frontend
├── go.mod
├── go.sum
├── internal
├── key.pem
├── main.go
├── rootCA.crt
├── rootCA.key
├── rootCA.srl
├── san.cnf
├── server.csr
└── wails.json

4 directories, 13 files


```

- If you are on Linux, you can use this command
```

wails dev -tags webkit2_41

```
