FROM --platform=$BUILDPLATFORM golang:alpine AS build

WORKDIR /app

RUN apk add --no-cache python3 bash
RUN python3 -m venv venv

COPY ./requirements.txt ./requirements.txt
RUN . ./venv/bin/activate && pip install -r requirements.txt

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
--mount=type=cache,target=/go/pkg . ./venv/bin/activate && python ./build.py --project wsvpn --platform=${TARGETOS} --architecture ${TARGETARCH}${TARGETVARIANT}

# |===========================================| #

FROM alpine:latest

RUN apk add --no-cache ca-certificates curl bash

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

COPY --from=build --chown=0:0 --chmod=755 /app/dist/wsvpn-$TARGETOS-$TARGETARCH$TARGETVARIENT /wsvpn
VOLUME /config

WORKDIR /config
ENTRYPOINT [ "/wsvpn" ]
