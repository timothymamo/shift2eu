// Command shift2eu-web stands in for a sibling composition root of
// cmd/shift2eu: it imports nothing, and test/architecture's composition-root
// probe proves the composition-root rule does not hold it at all, so a rule
// widened to **/cmd/** or to the cmd directory fails there.
package main

func main() {}
