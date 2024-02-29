FROM golang:1.22-bookworm

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -buildvcs=false -o /asklepios .

FROM scratch
COPY --from=0 /asklepios /bin/asklepios
USER 1000

CMD ["/bin/asklepios"]
