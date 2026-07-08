# The undump agent. Deployed alongside the client's backups.
# Needs access to the host's docker.sock to spin up ephemeral restore containers.
# pg_restore/psql run INSIDE the ephemeral container via docker exec —
# so a postgres client (postgresql-client) has no place in this image.
FROM golang:1.26 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/undump ./cmd/undump

FROM gcr.io/distroless/static-debian12
# Intentionally NOT :nonroot — access to the mounted docker.sock (see the run
# example below) requires root or a matching docker group GID on the host;
# the non-root variant was tested and breaks creation of ephemeral containers
# (permission denied on docker.sock), i.e. the agent's whole functionality.
COPY --from=builder /out/undump /usr/local/bin/undump

# Example run (daemon — checks each target on its own configured schedule):
#   docker run -v /var/run/docker.sock:/var/run/docker.sock \
#     -v $(pwd)/undump.yaml:/app/undump.yaml \
#     -e UNDUMP_API_KEY=... -e S3_ACCESS_KEY=... -e S3_SECRET_KEY=... \
#     ghcr.io/undump/agent run --config /app/undump.yaml
# For a single one-off pass instead, use `check` in place of `run`.
ENTRYPOINT ["/usr/local/bin/undump"]
CMD ["--help"]
