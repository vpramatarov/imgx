# ---- deps: cache go modules
FROM golang:1.26-trixie AS deps
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends \
      libheif-dev libaom-dev libde265-dev pkg-config git \
 && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download all

# ---- air-build: compile Air separately so its caches stay out of dev
FROM golang:1.26-trixie AS air-build
RUN go install github.com/air-verse/air@latest

# ---- dev: Go toolchain + libheif build deps + Air
FROM golang:1.26-trixie AS dev
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends \
      libheif-dev libaom-dev libde265-dev pkg-config \
 && rm -rf /var/lib/apt/lists/*
COPY --from=air-build /go/bin/air /usr/local/bin/air
ENV CGO_ENABLED=1
CMD ["air", "-c", ".air.toml"]

# ---- builder: CGO build with libheif + AVIF encode/decode
FROM golang:1.26-trixie AS builder
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends \
      libheif-dev libaom-dev libde265-dev pkg-config \
 && rm -rf /var/lib/apt/lists/*
COPY --from=deps /go/pkg/mod /go/pkg/mod
COPY . .
RUN CGO_ENABLED=1 GOOS=linux go mod tidy && go build \
      -tags heif \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/imgx ./cmd/imgx

# ---- runtime-libs: stage libheif, its plugins, and the full .so closure
# libheif >=1.17 dlopen()s codec plugins from /usr/lib/.../libheif/plugins/
# at runtime; those plugin .so's are not linked against libheif.so.1, so
# `ldd libheif.so.1` does NOT list them. We must install the plugin
# packages explicitly and run ldd on each plugin as well to pick up
# libde265/libaom/libdav1d and their transitive deps.
# libheif-plugin-x265 is intentionally omitted: x265 is GPL and we only
# need HEVC *decode* (plugin-libde265) for HEIC input. AVIF encode/decode
# is covered by the aomenc/aomdec plugins.
FROM debian:trixie-slim AS runtime-libs
RUN apt-get update && apt-get install -y --no-install-recommends \
      libheif1 \
      libheif-plugin-libde265 \
      libheif-plugin-aomdec \
      libheif-plugin-aomenc \
 && rm -rf /var/lib/apt/lists/*
RUN set -eux; \
    mkdir -p /stage/lib /stage/plugins; \
    # Copy libheif itself with its versioned symlink intact.
    cp -a /usr/lib/x86_64-linux-gnu/libheif.so* /stage/lib/; \
    # Copy the plugin tree verbatim (distroless has no package manager,
    # so libheif must find plugins at the exact same absolute path).
    cp -a /usr/lib/x86_64-linux-gnu/libheif/. /stage/plugins/; \
    # Walk the transitive shared-lib closure of libheif AND every plugin
    # via ldd. `cp -L` dereferences each resolved path so the real file
    # lands under its SONAME name (e.g. libaom.so.3) — that's what the
    # runtime loader looks for. linux-vdso and "not found" entries are
    # filtered by awk's NF==4 check (they have fewer/more fields).
    { \
      echo /usr/lib/x86_64-linux-gnu/libheif.so.1; \
      find /usr/lib/x86_64-linux-gnu/libheif -name '*.so*' -type f; \
    } \
      | xargs -r -I{} ldd {} \
      | awk 'NF==4 {print $3}' \
      | sort -u \
      | while read -r lib; do cp -L "$lib" /stage/lib/; done

# ---- prod: distroless/cc (glibc + libstdc++ + libgcc)
# distroless/cc-debian13 matches the trixie libs staged above.
# After copying the transitive closure + plugins + imgx binary: ~50–60 MB.
FROM gcr.io/distroless/cc-debian13:nonroot AS prod
COPY --from=runtime-libs /stage/lib/     /usr/lib/x86_64-linux-gnu/
COPY --from=runtime-libs /stage/plugins/ /usr/lib/x86_64-linux-gnu/libheif/
COPY --from=builder      /out/imgx       /imgx
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/imgx"]
CMD ["serve", "--addr", "0.0.0.0:8080"]
