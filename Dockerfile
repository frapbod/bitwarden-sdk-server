FROM golang:1.27 AS build
WORKDIR /workspace
COPY . .
RUN apt update && apt install unzip musl-tools ca-certificates -y
RUN update-ca-certificates
# Go 1.27.4 rejects cgo archives with LLVM bitcode sections (golang/go#83126) and
# sdk-go v2.1.0 ships them. Remove only those sections; the native code stays the same.
RUN go mod download github.com/bitwarden/sdk-go/v2 \
 && objcopy --remove-section=.llvmbc --remove-section=.llvmcmd \
    "$(go list -m -f '{{.Dir}}' github.com/bitwarden/sdk-go/v2)/internal/cinterface/lib/linux-$(go env GOARCH | sed s/amd64/x64/)/libbitwarden_c.a"
RUN make build-docker

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /workspace/bin/bitwarden-sdk-server .

EXPOSE 9998
ENTRYPOINT [ "/bitwarden-sdk-server", "serve" ]
