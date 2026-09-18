# Security policy

This is a local paper-trading research project. Never submit API keys,
access tokens, request tokens, account identifiers, or private run logs in a
public issue or pull request.

If you find a credential-handling or paper-safety issue, remove any exposed
credential immediately, revoke/rotate it with the provider, and contact the
maintainer privately before publishing details.

The dashboard's browser credential save is opt-in local storage and is not
encrypted. Use it only on a trusted machine. The application intentionally
does not provide live order execution.
