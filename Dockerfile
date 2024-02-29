FROM        golang:1.22-bookworm
ARG         ASKLEPIOS_TAG
ENV         ASKLEPIOS_TAG ${ASKLEPIOS_TAG}
ARG         ASKLEPIOS_ID
ENV         ASKLEPIOS_ID ${ASKLEPIOS_ID}
WORKDIR     /app
COPY        . .
RUN         CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
            go build -buildvcs=false -o /asklepios \
            -ldflags="-X 'github.com/iorchard/asklepios/cmd.ver=${ASKLEPIOS_VERSION}' -X 'github.com/iorchard/asklepios/cmd.id=${ASKLEPIOS_ID}'" .

FROM        scratch
COPY        --from=0 /asklepios /bin/asklepios
USER        1000

ENTRYPOINT  ["/bin/asklepios"]
CMD         ["serve"]
