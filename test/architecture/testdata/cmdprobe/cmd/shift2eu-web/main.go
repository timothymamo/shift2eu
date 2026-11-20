// Command shift2eu-web stands in for a sibling composition root of
// cmd/shift2eu: it imports the same third-party library and cloud SDK its sibling
// cmd/shift2eu imports, which the composition-root rule would refuse if it held
// this package. Test/architecture's composition-root probe proves the rule does
// not hold it at all, so a rule widened to **/cmd/** or to the cmd directory fails
// there twice: by the packages it holds and by the refusals it reports.
package main

import (
	"os"

	"example.com/logging"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

func main() {
	logging.Printf("%v %v", os.Args, ec2.Client{})
}
