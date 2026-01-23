<h1 align="center">Benchmarkoor</h1>

<p align="center">
  <img src="ui/public/img/logo_white.png" alt="Dispatchoor Logo" width="400">
</p>

## Overview

Benchmarkoor is a benchmarking tool for Ethereum execution clients. It runs standardized tests against multiple clients (Geth, Nethermind, Besu, Erigon, Reth, Nimbus) in isolated Docker containers and collects performance metrics.

## Documentation

- [Configuration Reference](docs/configuration.md) - All configuration options explained
- [Docker Guide](docs/docker.md) - Docker setup, requirements, and troubleshooting

## Docker Quickstart

The easiest way to get started is using Docker Compose:

```bash
make docker-up
```

This builds and starts:
- **benchmarkoor** - Runs benchmarks using [config.example.docker.yaml](config.example.docker.yaml)
- **ui** - Web UI available at http://localhost:8080

Results are saved to the `./results` directory.

To stop the services:

```bash
make docker-down
```

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
