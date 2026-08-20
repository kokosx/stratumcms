# Security

Report vulnerabilities through the repository's private security-reporting channel when one is available; do not publish exploit details in a public issue before a fix exists.

- Put Stratum behind HTTPS in production and enable secure cookies.
- Never expose or serve the data directory directly.
- Treat backup archives as sensitive because they contain users, password hashes, content, and media.
- Custom CSS is a deliberate trusted-administrator escape hatch.
- Image uploads are limited to decoded JPEG, PNG, and GIF content; SVG and arbitrary files are rejected.
- Keep the binary and its host operating system patched, restrict data-directory permissions, and test restores regularly.
