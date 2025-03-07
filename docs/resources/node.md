---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "neo4j_node Resource - terraform-provider-neo4j"
subcategory: ""
description: |-
  Neo4j Node, details: https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-node
---

# neo4j_node (Resource)

Neo4j Node, details: https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-node

## Example Usage

```terraform
resource "neo4j_node" "example" {
  labels = ["foo", "bar"]
  properties = {
    foo  = 100
    bar  = "qux"
    quxx = 1.2
  }
}

resource "neo4j_node" "example_without_labels" {
  properties = {
    foo = 100
  }
}

resource "neo4j_node" "example_without_properties" {
  labels = ["foo"]
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `labels` (List of String) Node labels, details: https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-labels
- `properties` (Map of String) Node properties, details: https://neo4j.com/docs/getting-started/appendix/graphdb-concepts/#graphdb-properties

### Read-Only

- `id` (String) Node unique identifier.
