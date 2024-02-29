#!/bin/bash
set -e

ASKLEPIOS_TAG=$(git describe --tags --abbrev=0 2>/dev/null)
if [ -z "${ASKLEPIOS_TAG}" ]; then
  echo "Abort: no git tag is found."
  exit 1
fi
ASKLEPIOS_ID=$(git rev-list -n 1 ${ASKLEPIOS_TAG})

docker build \
  -t jijisa/asklepios:${ASKLEPIOS_TAG} \
  --build-arg ASKLEPIOS_TAG="${ASKLEPIOS_TAG}" \
  --build-arg ASKLEPIOS_ID="${ASKLEPIOS_ID}" \
  --file Dockerfile \
  .

docker tag jijisa/asklepios:${ASKLEPIOS_TAG} jijisa/asklepios:latest
