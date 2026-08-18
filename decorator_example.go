package main

import "fmt"

// Component interface
type Component interface {
	Operation() string
}

// ConcreteComponent is the base implementation
type ConcreteComponent struct{}

func (c *ConcreteComponent) Operation() string {
	return "ConcreteComponent"
}

// Decorator embeds Component and adds behavior
type Decorator struct {
	component Component
}

func (d *Decorator) Operation() string {
	return d.component.Operation()
}

// ConcreteDecoratorA adds additional behavior
type ConcreteDecoratorA struct {
	Decorator
}

func NewConcreteDecoratorA(c Component) *ConcreteDecoratorA {
	return &ConcreteDecoratorA{
		Decorator: Decorator{component: c},
	}
}

func (d *ConcreteDecoratorA) Operation() string {
	return "ConcreteDecoratorA(" + d.Decorator.Operation() + ")"
}

// ConcreteDecoratorB adds different behavior
type ConcreteDecoratorB struct {
	Decorator
}

func NewConcreteDecoratorB(c Component) *ConcreteDecoratorB {
	return &ConcreteDecoratorB{
		Decorator: Decorator{component: c},
	}
}

func (d *ConcreteDecoratorB) Operation() string {
	return "ConcreteDecoratorB(" + d.Decorator.Operation() + ")"
}

func main() {
	// Create base component
	simple := &ConcreteComponent{}
	fmt.Println("Simple:", simple.Operation())

	// Decorate with A
	decoratorA := NewConcreteDecoratorA(simple)
	fmt.Println("Decorator A:", decoratorA.Operation())

	// Decorate with B
	decoratorB := NewConcreteDecoratorB(simple)
	fmt.Println("Decorator B:", decoratorB.Operation())

	// Chain decorators
	decoratorAB := NewConcreteDecoratorA(NewConcreteDecoratorB(simple))
	fmt.Println("Decorator A(B):", decoratorAB.Operation())

	decoratorBA := NewConcreteDecoratorB(NewConcreteDecoratorA(simple))
	fmt.Println("Decorator B(A):", decoratorBA.Operation())
}
