package scoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// Evaluate evaluates a Go-like expression over an environment containing profile attributes and event aggregates.
// The result is converted to a float64 (true -> 1.0, false -> 0.0, or numeric values directly) and clamped to [outputMin, outputMax].
func Evaluate(exprStr string, env map[string]any, outputMin, outputMax float64) (float64, error) {
	node, err := parser.ParseExpr(exprStr)
	if err != nil {
		return 0, fmt.Errorf("parse expression: %w", err)
	}

	val, err := eval(node, env)
	if err != nil {
		return 0, fmt.Errorf("eval: %w", err)
	}

	var result float64
	switch v := val.(type) {
	case bool:
		if v {
			result = 1.0
		} else {
			result = 0.0
		}
	case float64:
		result = v
	case float32:
		result = float64(v)
	case int:
		result = float64(v)
	case int64:
		result = float64(v)
	case int32:
		result = float64(v)
	default:
		return 0, fmt.Errorf("expression returned unsupported type: %T", val)
	}

	// Clamp to [outputMin, outputMax]
	if result < outputMin {
		result = outputMin
	}
	if result > outputMax {
		result = outputMax
	}

	return result, nil
}

func eval(expr ast.Expr, env map[string]any) (any, error) {
	switch n := expr.(type) {
	case *ast.ParenExpr:
		return eval(n.X, env)

	case *ast.BasicLit:
		switch n.Kind {
		case token.INT, token.FLOAT:
			val, err := strconv.ParseFloat(n.Value, 64)
			if err != nil {
				return nil, err
			}
			return val, nil
		case token.STRING:
			val, err := strconv.Unquote(n.Value)
			if err != nil {
				return nil, err
			}
			return val, nil
		default:
			return nil, fmt.Errorf("unsupported literal type: %s", n.Kind)
		}

	case *ast.Ident:
		switch n.Name {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			val, ok := env[n.Name]
			if !ok {
				return nil, fmt.Errorf("identifier %q not found in environment", n.Name)
			}
			return val, nil
		}

	case *ast.SelectorExpr:
		return evalSelector(n, env)

	case *ast.UnaryExpr:
		val, err := eval(n.X, env)
		if err != nil {
			return nil, err
		}
		switch n.Op {
		case token.NOT: // !
			b, ok := val.(bool)
			if !ok {
				return nil, fmt.Errorf("operator ! expected bool, got %T", val)
			}
			return !b, nil
		case token.SUB: // -
			f, ok := toFloat(val)
			if !ok {
				return nil, fmt.Errorf("operator - expected number, got %T", val)
			}
			return -f, nil
		case token.ADD: // +
			f, ok := toFloat(val)
			if !ok {
				return nil, fmt.Errorf("operator + expected number, got %T", val)
			}
			return f, nil
		default:
			return nil, fmt.Errorf("unsupported unary operator: %s", n.Op)
		}

	case *ast.BinaryExpr:
		return evalBinary(n, env)

	default:
		return nil, fmt.Errorf("unsupported AST node type: %T", expr)
	}
}

func evalSelector(sel *ast.SelectorExpr, env map[string]any) (any, error) {
	left, err := eval(sel.X, env)
	if err != nil {
		return nil, err
	}

	var m map[string]any
	switch typedLeft := left.(type) {
	case map[string]any:
		m = typedLeft
	case json.RawMessage:
		if err := json.Unmarshal(typedLeft, &m); err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON map: %w", err)
		}
	default:
		return nil, fmt.Errorf("cannot select field %q from type %T", sel.Sel.Name, left)
	}

	val, ok := m[sel.Sel.Name]
	if !ok {
		return nil, fmt.Errorf("field %q not found", sel.Sel.Name)
	}
	return val, nil
}

func evalBinary(bin *ast.BinaryExpr, env map[string]any) (any, error) {
	leftVal, err := eval(bin.X, env)
	if err != nil {
		return nil, err
	}

	if bin.Op == token.LAND { // &&
		leftBool, ok := leftVal.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of && must be bool, got %T", leftVal)
		}
		if !leftBool {
			return false, nil
		}
		rightVal, err := eval(bin.Y, env)
		if err != nil {
			return nil, err
		}
		rightBool, ok := rightVal.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of && must be bool, got %T", rightVal)
		}
		return rightBool, nil
	}

	if bin.Op == token.LOR { // ||
		leftBool, ok := leftVal.(bool)
		if !ok {
			return nil, fmt.Errorf("left operand of || must be bool, got %T", leftVal)
		}
		if leftBool {
			return true, nil
		}
		rightVal, err := eval(bin.Y, env)
		if err != nil {
			return nil, err
		}
		rightBool, ok := rightVal.(bool)
		if !ok {
			return nil, fmt.Errorf("right operand of || must be bool, got %T", rightVal)
		}
		return rightBool, nil
	}

	rightVal, err := eval(bin.Y, env)
	if err != nil {
		return nil, err
	}

	if leftNum, isLeftNum := toFloat(leftVal); isLeftNum {
		rightNum, isRightNum := toFloat(rightVal)
		if !isRightNum {
			return nil, fmt.Errorf("type mismatch: left operand is number, right operand is %T", rightVal)
		}

		switch bin.Op {
		case token.ADD: // +
			return leftNum + rightNum, nil
		case token.SUB: // -
			return leftNum - rightNum, nil
		case token.MUL: // *
			return leftNum * rightNum, nil
		case token.QUO: // /
			if rightNum == 0 {
				return nil, errors.New("division by zero")
			}
			return leftNum / rightNum, nil
		case token.LSS: // <
			return leftNum < rightNum, nil
		case token.GTR: // >
			return leftNum > rightNum, nil
		case token.LEQ: // <=
			return leftNum <= rightNum, nil
		case token.GEQ: // >=
			return leftNum >= rightNum, nil
		case token.EQL: // ==
			return leftNum == rightNum, nil
		case token.NEQ: // !=
			return leftNum != rightNum, nil
		default:
			return nil, fmt.Errorf("unsupported binary operator for numbers: %s", bin.Op)
		}
	}

	if leftStr, isLeftStr := leftVal.(string); isLeftStr {
		rightStr, isRightStr := rightVal.(string)
		if !isRightStr {
			return nil, fmt.Errorf("type mismatch: left operand is string, right operand is %T", rightVal)
		}

		switch bin.Op {
		case token.EQL: // ==
			return leftStr == rightStr, nil
		case token.NEQ: // !=
			return leftStr != rightStr, nil
		case token.ADD: // + (string concatenation)
			return leftStr + rightStr, nil
		default:
			return nil, fmt.Errorf("unsupported binary operator for strings: %s", bin.Op)
		}
	}

	if leftBool, isLeftBool := leftVal.(bool); isLeftBool {
		rightBool, isRightBool := rightVal.(bool)
		if !isRightBool {
			return nil, fmt.Errorf("type mismatch: left operand is bool, right operand is %T", rightVal)
		}

		switch bin.Op {
		case token.EQL: // ==
			return leftBool == rightBool, nil
		case token.NEQ: // !=
			return leftBool != rightBool, nil
		default:
			return nil, fmt.Errorf("unsupported binary operator for booleans: %s", bin.Op)
		}
	}

	return nil, fmt.Errorf("unsupported binary operands: left=%T, right=%T", leftVal, rightVal)
}

func toFloat(val any) (float64, bool) {
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	}
	return 0, false
}

// ExtractEventAggregates parses the Go-like expression and returns a map of event types to their required day intervals (e.g. {"click": [30], "purchase": [90]})
func ExtractEventAggregates(exprStr string) (map[string][]int, error) {
	if exprStr == "" {
		return nil, nil
	}
	node, err := parser.ParseExpr(exprStr)
	if err != nil {
		return nil, err
	}
	res := make(map[string][]int)
	ast.Inspect(node, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// We are looking for something like events.click.count_30d
		// sel.X is events.click (which is also a SelectorExpr), sel.Sel is count_30d
		innerSel, ok := sel.X.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := innerSel.X.(*ast.Ident)
		if !ok || ident.Name != "events" {
			return true
		}
		eventType := innerSel.Sel.Name
		aggName := sel.Sel.Name

		// Parse count_<days>d
		var days int
		if _, err := fmt.Sscanf(aggName, "count_%dd", &days); err == nil {
			// Avoid duplicates in the list of days
			found := false
			for _, d := range res[eventType] {
				if d == days {
					found = true
					break
				}
			}
			if !found {
				res[eventType] = append(res[eventType], days)
			}
		}
		return true
	})
	return res, nil
}

// BuildExpressionEnv prepares the evaluation environment for a profile
func BuildExpressionEnv(ctx context.Context, store ports.Store, tenantID, workspaceID string, profile domain.Profile, exprStr string) (map[string]any, error) {
	var profileAttrs map[string]any
	if len(profile.Attributes) > 0 {
		_ = json.Unmarshal(profile.Attributes, &profileAttrs)
	}
	if profileAttrs == nil {
		profileAttrs = make(map[string]any)
	}

	env := map[string]any{
		"profile": profileAttrs,
	}

	// Extract event aggregates from expression
	aggregates, err := ExtractEventAggregates(exprStr)
	if err != nil {
		return nil, err
	}

	if len(aggregates) > 0 {
		eventsEnv := make(map[string]any)
		for eventType, daysList := range aggregates {
			eventMap := make(map[string]any)
			for _, days := range daysList {
				count, err := store.GetEventCount(ctx, tenantID, workspaceID, profile.ExternalID, profile.AnonymousID, eventType, days)
				if err != nil {
					return nil, err
				}
				eventMap[fmt.Sprintf("count_%dd", days)] = float64(count)
			}
			eventsEnv[eventType] = eventMap
		}
		env["events"] = eventsEnv
	}

	return env, nil
}

