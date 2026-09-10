# Examples

`tfplugindocs` embeds these files verbatim into `docs/`, so editing one changes the published documentation. Run `make docs` afterwards and commit the result, or CI fails.

* `provider/provider.tf` — the provider index page
* `data-sources/<data source name>/data-source.tf` — that data source's page
* `resources/<resource name>/resource.tf` — that resource's page
* `resources/<resource name>/import.sh`, `import-by-identity.tf`, `import-by-string-id.tf` — that resource's Import section

Any other `.tf` file in this directory is ignored by the doc generator.
