# Create the relationship from the node to itself.
resource "neo4j_node" "example" {}

resource "neo4j_relationship" "with_props" {
  type          = "foo"
  start_node_id = neo4j_node.example.id
  end_node_id   = neo4j_node.example.id
  properties = {
    foo  = 100
    bar  = "qux"
    quxx = 1.2
  }
}

resource "neo4j_relationship" "without_props" {
  type          = "foo"
  start_node_id = neo4j_node.example.id
  end_node_id   = neo4j_node.example.id
}
