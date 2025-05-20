package jq

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/sirupsen/logrus"
)

// Query contains the data and logic needed to evaluate a query. Instances are created using the Compile or Evaluate
// methods of the Tool type.
type Query struct {
	logger    *logrus.Logger
	source    string
	variables []string
	code      *gojq.Code
}

// Evaluate evaluates the query with on the given input. The output should be a pointer to a variable where the result
// will be stored. Optional named variables can be passed.
func (q *Query) Evaluate(input any, output any, variables ...Variable) error {
	slices.SortFunc(variables, func(a, b Variable) int {
		return strings.Compare(a.name, b.name)
	})
	names := make([]string, len(variables))
	values := make([]any, len(variables))
	for i, variable := range variables {
		names[i] = variable.name
		values[i] = variable.value
	}
	if !reflect.DeepEqual(names, q.variables) {
		return fmt.Errorf(
			"query was compiled with variables %s but used with %s",
			q.variables, names,
		)
	}
	return q.evaluate(input, output, values)
}

func (q *Query) evaluate(input any, output any, variables []any) error {
	// Check that the output is a pointer:
	outputType := reflect.TypeOf(output)
	if outputType.Kind() != reflect.Pointer {
		return fmt.Errorf("output should be a pointer, but it is of type '%T'", output)
	}

	// The library that we use expects the input to bo composed only of primitive types, slices and maps, but we
	// want to support other input types, like structs. To achieve that we serialize the input and deserialize it
	// again.
	var tmp any
	err := q.convert(input, &tmp)
	if err != nil {
		return err
	}
	input = tmp

	// Run the query:
	var results []any
	iter := q.code.Run(input, variables...)
	for {
		result, ok := iter.Next()
		if !ok {
			break
		}
		err, ok = result.(error)
		if ok {
			return err
		}
		results = append(results, result)
	}

	// If the output isn't a slice then we take only the first result:
	var result any
	if outputType.Elem().Kind() == reflect.Slice {
		result = results
	} else {
		length := len(results)
		if length == 0 {
			return fmt.Errorf("query produced no results")
		}
		if length > 1 {
			q.logger.WithFields(logrus.Fields{
				"query":   q.source,
				"type":    fmt.Sprintf("%T", output),
				"results": results,
			}).Warn(
				"Query produced multiple results but output type isn't a slice, will return the " +
					"first result",
			)
		}
		result = results[0]
	}

	// Copy the result to the output:
	return q.convert(result, output)
}

func (q *Query) convert(input any, output any) error {
	switch input := input.(type) {
	case bool:
		switch output := output.(type) {
		case *bool:
			*output = input
		case *any:
			*output = input
		default:
			return fmt.Errorf("can't convert boolean to %T", output)
		}
	case int:
		switch output := output.(type) {
		case *int:
			*output = input
		case *int32:
			*output = int32(input) // nolint: gosec
		case *int64:
			*output = int64(input)
		case *any:
			*output = input
		default:
			return fmt.Errorf("can't convert integer to %T", output)
		}
	case float64:
		switch output := output.(type) {
		case *int:
			result := math.Floor(input)
			*output = int(result)
			if result != input {
				q.logger.Warn(
					"Conversion from float to integer loses precision",
					slog.Float64("input", input),
					slog.Int("output", *output),
				)
			}
		case *int32:
			result := math.Floor(input)
			*output = int32(result)
			if result != input {
				q.logger.Warn(
					"Conversion from float to 32 bits integer loses precision",
					slog.Float64("input", input),
					slog.Int("output", int(*output)),
				)
			}
		case *int64:
			result := math.Floor(input)
			*output = int64(result)
			if result != input {
				q.logger.Warn(
					"Conversion from float to 64 bits integer loses precision",
					slog.Float64("input", input),
					slog.Int64("output", *output),
				)
			}
		case *float64:
			*output = input
		case *any:
			*output = input
		default:
			return fmt.Errorf("failed to convert float to %T", output)
		}
	case string:
		switch output := output.(type) {
		case *string:
			*output = input
		case *any:
			*output = input
		default:
			return fmt.Errorf("failed to convert string to %T", output)
		}
	case []any:
		switch output := output.(type) {
		case *[]any:
			*output = input
		case *any:
			*output = input
		default:
			return q.clone(input, output)
		}
	case map[string]any:
		switch output := output.(type) {
		case *map[string]any:
			*output = input
		case *any:
			*output = input
		default:
			return q.clone(input, output)
		}
	default:
		return q.clone(input, output)
	}
	return nil
}

func (q *Query) clone(input any, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, output)
}
