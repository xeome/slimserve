# Builder and runner tags: https://bob.hex.pm/docker?repo=hexpm/elixir&os=debian
# Keep the versions in step with .tool-versions.
ARG ELIXIR_VERSION=1.20.5
ARG OTP_VERSION=29.1.2
ARG DEBIAN_VERSION=trixie-20261104-slim

ARG BUILDER_IMAGE="docker.io/hexpm/elixir:${ELIXIR_VERSION}-erlang-${OTP_VERSION}-debian-${DEBIAN_VERSION}"
ARG RUNNER_IMAGE="docker.io/debian:${DEBIAN_VERSION}"

FROM ${BUILDER_IMAGE} AS builder

RUN apt-get update \
  && apt-get install -y --no-install-recommends build-essential git \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /app

RUN mix local.hex --force \
  && mix local.rebar --force

ENV MIX_ENV="prod"

COPY mix.exs mix.lock ./
RUN mix deps.get --only $MIX_ENV
RUN mkdir config

# Compile-time config goes in before the dependencies compile, so a change to it recompiles them.
COPY config/config.exs config/${MIX_ENV}.exs config/
RUN mix deps.compile

RUN mix assets.setup

COPY priv priv
COPY lib lib

# Before the assets: compiling writes the colocated hooks that app.js imports.
RUN mix compile

COPY assets assets
RUN mix assets.deploy

# Changes to config/runtime.exs don't require recompiling the code
COPY config/runtime.exs config/
COPY rel rel

RUN mix release

FROM ${RUNNER_IMAGE} AS final

# inotify-tools: SlimServe.Watcher runs inotifywait for the live listing.
RUN apt-get update \
  && apt-get install -y --no-install-recommends libstdc++6 openssl libncurses6 locales ca-certificates inotify-tools \
  && rm -rf /var/lib/apt/lists/*

# A UTF-8 locale, or the VM reads file names as latin1 and mangles non-ASCII names in a listing.
RUN sed -i '/en_US.UTF-8/s/^# //g' /etc/locale.gen \
  && locale-gen

ENV LANG=en_US.UTF-8
ENV LANGUAGE=en_US:en
ENV LC_ALL=en_US.UTF-8

# The same non-root uid as the Go image, so a volume prepared for one works with the other.
RUN groupadd --gid 1001 slimserve \
  && useradd --uid 1001 --gid 1001 --no-create-home --home-dir /app slimserve \
  && mkdir /data \
  && chown slimserve:slimserve /data

WORKDIR "/app"
RUN chown slimserve /app

COPY --from=builder --chown=slimserve:root /app/_build/prod/rel/slimserve ./

USER slimserve

ENV PHX_SERVER=true
ENV PORT=8080
ENV SLIMSERVE_ROOT=/data
EXPOSE 8080

CMD ["/app/bin/slimserve", "start"]
