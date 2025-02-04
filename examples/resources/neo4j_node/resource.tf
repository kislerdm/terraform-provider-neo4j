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
