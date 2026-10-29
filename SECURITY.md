# Security

pbipx exists to keep sensitive values out of git history and out of AI agents'
transcripts. A bug that lets a value through is a security issue. That covers a
missed context, a value printed in output or an error, and a gate that fails
open.

Until this repository has a public issue tracker, report privately to the
maintainer. **Never include the leaked value itself**: describe the file
structure and reproduce it with synthetic data
([CONTRIBUTING.md](CONTRIBUTING.md#synthetic-data)).

The [threat model](docs/design/threat-model.md) says what pbipx does and does
not protect against.
