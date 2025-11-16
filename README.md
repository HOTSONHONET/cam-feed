# Cam-Feed

## About


## Instructions

### Commands to set vite build for wails

```
npm install -D @tailwindcss/postcss
npm run build

```

### Commands to Self-Signed OpenSSL certificates 

- The cameras are only accessible via Browsers, inorder to run access camera browsers requires `https` based endpoints. You can generate a self-signed Openssl certificate and use just paste in the project directory.

    - Generate a private key for our root CA

        ```

        openssl genrsa -out rootCA.key 4096

        ```

    - Create root CA

        ```

        openssl req -x509 -new -nodes -key rootCA.key -sha256 -days 3650 -subj "/CN=CamFeed Root CA" -out rootCA.crt

        ```

    - Creating private key for server certificate


        ```

        openssl genrsa -out key.pem 2048

        ```

    - Create san.cnf file (boiler plate)

        ```

        [req]
        distinguished_name=req
        req_extensions=req_ext

        [req_ext]
        basicConstraints = CA:FALSE
        keyUsage = digitalSignature, keyEncipherment
        extendedKeyUsage = serverAuth
        subjectAltName = @alt_names

        [alt_names]
        DNS.1 = localhost
        IP.1  = 127.0.0.1
        IP.2  = <ip-address>        # your reserved LAN IP


        ```

    - Creating Certificate signing request

        ```

        openssl req -new -key key.pem -subj "/CN=<ip-address>" -out server.csr -config san.cnf

        ```

    - Sign the CSR using the Root CA

        ```

        openssl x509 -req -in server.csr \
        -CA rootCA.crt -CAkey rootCA.key -CAcreateserial \
        -out cert.pem -days 825 -sha256 \
        -extensions req_ext -extfile san.cnf


        ```

    - Verifying the chain

        ```

        openssl verify -CAfile rootCA.crt cert.pem


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
