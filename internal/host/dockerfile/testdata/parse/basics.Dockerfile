# syntax=docker/dockerfile:1
ARG BASE=alpine:3.20
FROM ${BASE} AS build
WORKDIR /src
COPY --chown=1000:1000 --link go.mod go.sum ./
RUN --mount=type=cache,target=/root/.cache go mod download
RUN go build \
    -o /out/app \
    ./cmd/app

FROM scratch
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
CMD ["--help"]
