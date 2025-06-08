#!/bin/sh
set -e

# Default config file location
CONFIG_FILE="${CONFIG_FILE:-/etc/slimserve/config.json}"

# Log which config file is being used
echo "SlimServe starting with config file: $CONFIG_FILE"

# Check if config file exists and is readable
if [ -f "$CONFIG_FILE" ]; then
    echo "Using configuration from: $CONFIG_FILE"
    echo "Warning: Config file loading not yet implemented, using CLI flags and environment variables only"
fi

# Use CLI flags and environment variables (config file loading not implemented yet)
exec /usr/local/bin/slimserve "$@"