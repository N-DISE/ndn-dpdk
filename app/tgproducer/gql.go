package tgproducer

import (
	"reflect"

	"github.com/graphql-go/graphql"
	"github.com/usnistgov/ndn-dpdk/app/tg/tggql"
	"github.com/usnistgov/ndn-dpdk/core/gqlserver"
	"github.com/usnistgov/ndn-dpdk/iface"
	"github.com/usnistgov/ndn-dpdk/ndn"
)

// GqlRetrieveByFaceID returns *Producer associated with a face.
// It is assigned during package tg initialization.
var GqlRetrieveByFaceID func(id iface.ID) interface{}

// GraphQL types.
var (
	GqlReplyInput          *graphql.InputObject
	GqlPatternInput        *graphql.InputObject
	GqlPatternCountersType *graphql.Object
	GqlCountersType        *graphql.Object
	GqlProducerNodeType    *gqlserver.NodeType
	GqlProducerType        *graphql.Object
)

func init() {
	GqlReplyInput = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "TgpReplyInput",
		Description: "Traffic generator producer reply definition.",
		Fields: gqlserver.BindInputFields(Reply{}, gqlserver.FieldTypes{
			reflect.TypeOf(ndn.Name{}): gqlserver.NonNullString,
		}),
	})
	GqlPatternInput = graphql.NewInputObject(graphql.InputObjectConfig{
		Name:        "TgpPatternInput",
		Description: "Traffic generator producer pattern definition.",
		Fields: gqlserver.BindInputFields(Pattern{}, gqlserver.FieldTypes{
			reflect.TypeOf(ndn.Name{}): gqlserver.NonNullString,
			reflect.TypeOf(Reply{}):    GqlReplyInput,
		}),
	})

	GqlPatternCountersType = graphql.NewObject(graphql.ObjectConfig{
		Name:   "TgpPatternCounters",
		Fields: gqlserver.BindFields(PatternCounters{}, nil),
	})
	GqlCountersType = graphql.NewObject(graphql.ObjectConfig{
		Name: "TgpCounters",
		Fields: gqlserver.BindFields(Counters{}, gqlserver.FieldTypes{
			reflect.TypeOf(PatternCounters{}): GqlPatternCountersType,
		}),
	})

	GqlProducerNodeType = tggql.NewNodeType((*Producer)(nil), &GqlRetrieveByFaceID)
	GqlProducerType = graphql.NewObject(GqlProducerNodeType.Annotate(graphql.ObjectConfig{
		Name:        "TgProducer",
		Description: "Traffic generator producer.",
		Fields: tggql.CommonFields(graphql.Fields{
			"patterns": &graphql.Field{
				Description: "Traffic patterns.",
				Type:        gqlserver.NonNullJSON,
				Resolve:     gqlserver.MethodResolver("Patterns"),
			},
			"counters": &graphql.Field{
				Description: "Counters.",
				Type:        graphql.NewNonNull(GqlCountersType),
				Resolve:     gqlserver.MethodResolver("Counters"),
			},
		}),
	}))
	GqlProducerNodeType.Register(GqlProducerType)
}
