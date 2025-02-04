# Terraform Provider to manage Neo4j Graphs

The provider to provision Neo4j [Nodes](https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-node)
and [Relationships](https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-relationship).

## Using the provider

```terraform
terraform {
  required_providers {
    neo4j = {
      source = "kislerdm/neo4j"
    }
  }
}

provider "neo4j" {}
```

### Authentication and Configuration

Configuration for the provider can be derived from several sources, which are applied in the following order:

1. Parameters in the provider configuration

```terraform
provider "neo4j" {
  db_uri      = "<Database URI>"
  db_user     = "<Database user name>"
  db_password = "<Database user password>"
  db_name     = "<Database name>"
}
```

2. Environment variables.

| Provider configuration | Environment variable | Meaning           | Required | Default |
|:-----------------------|:---------------------|:------------------|:--------:|:--------|
| `db_uri`               | `DB_URI`             | Database URI      |   true   | NA      |
| `db_user`              | `DB_USER`            | Database username |   true   | NA      |
| `db_password`          | `DB_PASSWORD`        | Database password |   true   | NA      |
| `db_name`              | `DB_NAME`            | Database name     |  false   | neo4j   |
