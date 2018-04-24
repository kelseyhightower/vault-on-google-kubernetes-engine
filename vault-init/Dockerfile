FROM golang:1.10.1
WORKDIR /go-build
COPY . .
RUN go get -u golang.org/x/vgo
RUN CGO_ENABLED=0 GOOS=linux vgo build

FROM scratch
COPY --from=0 /go-build/vault-init .
ENTRYPOINT ["/vault-init"]
