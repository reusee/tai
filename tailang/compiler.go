package tailang

import (
	"fmt"
	"io"
	"unicode"
)

type Compiler struct {
	tokenizer *Tokenizer
	curr      *Token
	next      *Token

	fun       *Function
	constants map[any]int

	arities map[string]int
}

func Compile(name string, r io.Reader) (*Function, error) {
	c := &Compiler{
		tokenizer: NewTokenizer(r),
		fun: &Function{
			Name: name,
			Code: make([]OpCode, 0),
		},
		constants: make(map[any]int),
		arities: map[string]int{
			"Log":  1,
			"Add":  2,
			"Sub":  2,
			"Mul":  2,
			"Div":  2,
			"Mod":  2,
			"Eq":   2,
			"Ne":   2,
			"Gt":   2,
			"Lt":   2,
			"Ge":   2,
			"Le":   2,
			"Not":  1,
			"And":  2,
			"Or":   2,
			"Join": 1, // Join list
			"Len":  1,
		},
	}

	// Initialize lookahead
	if err := c.init(); err != nil {
		return nil, err
	}

	for !c.isAtEnd() {
		if err := c.parseStatement(); err != nil {
			return nil, err
		}
	}

	c.emit(OpReturn)

	return c.fun, nil
}

func (c *Compiler) init() error {
	tok, err := c.tokenizer.Current()
	if err != nil {
		return err
	}
	c.next = tok
	c.tokenizer.Consume()
	return c.advance()
}

func (c *Compiler) advance() error {
	c.curr = c.next
	tok, err := c.tokenizer.Current()
	if err != nil {
		return err
	}
	c.next = tok
	c.tokenizer.Consume()
	return nil
}

func (c *Compiler) isAtEnd() bool {
	return c.curr.Kind == TokenEOF
}

func (c *Compiler) match(kind TokenKind) bool {
	if c.curr.Kind == kind {
		c.advance()
		return true
	}
	return false
}

func (c *Compiler) matchText(text string) bool {
	if c.curr.Kind == TokenIdentifier && c.curr.Text == text {
		c.advance()
		return true
	}
	return false
}

func (c *Compiler) consume(kind TokenKind, errMsg string) error {
	if c.curr.Kind == kind {
		return c.advance()
	}
	return fmt.Errorf("%s, got %v (%q)", errMsg, c.curr.Kind, c.curr.Text)
}

func (c *Compiler) emit(op OpCode) {
	c.fun.Code = append(c.fun.Code, op)
}

func (c *Compiler) makeConstant(val any) int {
	if idx, ok := c.constants[val]; ok {
		return idx
	}
	idx := len(c.fun.Constants)
	c.fun.Constants = append(c.fun.Constants, val)
	c.constants[val] = idx
	return idx
}

func (c *Compiler) parseStatement() error {
	if c.curr.Kind == TokenIdentifier {
		switch c.curr.Text {
		case "Def":
			return c.parseDef()
		case "Set":
			return c.parseSet()
		case "If":
			return c.parseIf()
		case "Return":
			c.advance()
			if !c.isAtEnd() && c.curr.Text != "}" { // heuristics for end of statement
				if err := c.parseExpression(); err != nil {
					return err
				}
			} else {
				// Return nil
				idx := c.makeConstant(nil)
				c.emit(OpLoadConst.With(idx))
			}
			c.emit(OpReturn)
			return nil
		}
	}

	if err := c.parseExpression(); err != nil {
		return err
	}
	// Note: We don't pop result, allowing expression sequence to accumulate on stack
	// or return last value.
	return nil
}

func (c *Compiler) parseDef() error {
	c.advance() // consume Def
	if c.curr.Kind != TokenIdentifier {
		return fmt.Errorf("expected identifier after Def")
	}
	name := c.curr.Text
	c.advance()

	if err := c.parseExpression(); err != nil {
		return err
	}

	idx := c.makeConstant(name)
	c.emit(OpDefVar.With(idx))
	return nil
}

func (c *Compiler) parseSet() error {
	c.advance() // consume Set
	if c.curr.Kind != TokenIdentifier {
		return fmt.Errorf("expected identifier after Set")
	}
	name := c.curr.Text
	c.advance()

	if err := c.parseExpression(); err != nil {
		return err
	}

	idx := c.makeConstant(name)
	c.emit(OpSetVar.With(idx))
	return nil
}

func (c *Compiler) parseIf() error {
	c.advance() // consume If

	// Condition
	if err := c.parseExpression(); err != nil {
		return err
	}

	// JumpFalse placeholder
	jumpFalseIdx := len(c.fun.Code)
	c.emit(OpJumpFalse.With(0))

	// True Block
	if err := c.parseBlockBody(); err != nil {
		return err
	}

	// Jump placeholder (skip Else)
	jumpIdx := len(c.fun.Code)
	c.emit(OpJump.With(0))

	// Patch JumpFalse
	falseOffset := len(c.fun.Code) - jumpFalseIdx - 1
	c.fun.Code[jumpFalseIdx] = OpJumpFalse.With(falseOffset)

	// Else Block
	if c.curr.Kind == TokenIdentifier && c.curr.Text == "Else" {
		c.advance()
		if err := c.parseBlockBody(); err != nil {
			return err
		}
	}

	// Patch Jump
	endOffset := len(c.fun.Code) - jumpIdx - 1
	c.fun.Code[jumpIdx] = OpJump.With(endOffset)

	return nil
}

// parseBlockBody parses { ... } or a single statement?
// For If, usually { ... }
func (c *Compiler) parseBlockBody() error {
	if c.curr.Kind != TokenSymbol || c.curr.Text != "{" {
		return fmt.Errorf("expected '{'")
	}
	c.advance()

	for !c.isAtEnd() && (c.curr.Kind != TokenSymbol || c.curr.Text != "}") {
		if err := c.parseStatement(); err != nil {
			return err
		}
	}

	return c.consume(TokenSymbol, "expected '}'")
}

func (c *Compiler) parseExpression() error {
	if err := c.parseTerm(); err != nil {
		return err
	}

	for c.curr.Kind == TokenSymbol && c.curr.Text == "|" {
		c.advance()
		// Pipe to next Call
		// Expects an Identifier (Function)
		if c.curr.Kind != TokenIdentifier {
			return fmt.Errorf("expected function identifier after '|'")
		}
		// logic handled in parseTerm's call handling? No.
		// A | B means B is called with A as first argument.
		// We parse B as a function call, but we reduce expected arity by 1.
		if err := c.parseCall(1); err != nil {
			return err
		}
	}

	// Handle infix math: A + B
	if c.curr.Kind == TokenSymbol && (c.curr.Text == "+" || c.curr.Text == "-" || c.curr.Text == "*" || c.curr.Text == "/") {
		op := c.curr.Text
		c.advance()
		if err := c.parseExpression(); err != nil { // RHS
			return err
		}
		// Resolve Op
		var funcName string
		switch op {
		case "+":
			funcName = "Add"
		case "-":
			funcName = "Sub"
		case "*":
			funcName = "Mul"
		case "/":
			funcName = "Div"
		}
		idx := c.makeConstant(funcName)
		c.emit(OpLoadVar.With(idx))
		c.emit(OpCall.With(2))
	}

	return nil
}

func (c *Compiler) parseTerm() error {
	switch c.curr.Kind {
	case TokenNumber:
		idx := c.makeConstant(c.curr.Value)
		c.emit(OpLoadConst.With(idx))
		c.advance()

	case TokenString:
		idx := c.makeConstant(c.curr.Text)
		c.emit(OpLoadConst.With(idx))
		c.advance()

	case TokenIdentifier:
		// Check for function call vs variable
		name := c.curr.Text
		firstRune := []rune(name)[0]
		if unicode.IsUpper(firstRune) {
			return c.parseCall(0)
		}
		// Variable
		idx := c.makeConstant(name)
		c.emit(OpLoadVar.With(idx))
		c.advance()

	case TokenSymbol:
		switch c.curr.Text {
		case "[":
			return c.parseList()
		case "{":
			return c.parseClosure()
		case "(":
			c.advance()
			if err := c.parseExpression(); err != nil {
				return err
			}
			return c.consume(TokenSymbol, "expected ')'")
		default:
			return fmt.Errorf("unexpected symbol %s", c.curr.Text)
		}

	default:
		return fmt.Errorf("unexpected token %v", c.curr)
	}
	return nil
}

func (c *Compiler) parseCall(pipedArgs int) error {
	name := c.curr.Text
	// Don't consume yet, need for error map

	arity, ok := c.arities[name]
	if !ok {
		arity = 1 // Default
	}

	c.advance() // consume name

	needed := arity - pipedArgs
	if needed < 0 {
		return fmt.Errorf("too many piped arguments for %s", name)
	}

	idx := c.makeConstant(name)
	c.emit(OpLoadVar.With(idx))

	if pipedArgs > 0 {
		// Stack state: [Arg0, Func]
		// We need: [Func, Arg0]
		// OpSwap swaps top 2 elements.
		c.emit(OpSwap)
	}

	for range needed {
		if err := c.parseTerm(); err != nil {
			return err
		}
	}

	c.emit(OpCall.With(arity))
	return nil
}

func (c *Compiler) parseList() error {
	c.advance() // consume [

	count := 0
	for !c.isAtEnd() && (c.curr.Kind != TokenSymbol || c.curr.Text != "]") {
		if err := c.parseTerm(); err != nil { // List elements are terms/expressions
			return err
		}
		count++
	}

	if err := c.consume(TokenSymbol, "expected ']'"); err != nil {
		return err
	}

	c.emit(OpMakeList.With(count))
	return nil
}

func (c *Compiler) parseClosure() error {
	// Treat { ... } as a lambda
	// Compile a new Function
	parentFun := c.fun
	parentConsts := c.constants

	newFun := &Function{
		Name: fmt.Sprintf("%s_lambda_%d", parentFun.Name, len(parentFun.Code)),
		Code: make([]OpCode, 0),
	}

	c.fun = newFun
	c.constants = make(map[any]int)

	// Parse Body
	if err := c.parseBlockBody(); err != nil {
		return err
	}
	// Implicit return?
	// If last opcode is not OpReturn, add OpReturn (nil?)
	if len(c.fun.Code) == 0 || (c.fun.Code[len(c.fun.Code)-1]&0xff) != OpReturn {
		// Load nil?
		idx := c.makeConstant(nil)
		c.emit(OpLoadConst.With(idx))
		c.emit(OpReturn)
	}

	compiledFun := c.fun

	// Restore
	c.fun = parentFun
	c.constants = parentConsts

	// Emit MakeClosure
	idx := c.makeConstant(compiledFun)
	c.emit(OpMakeClosure.With(idx))

	return nil
}
