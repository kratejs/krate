package diag

// Events, forms and refs (KR5xxx).
const (
	// CodeUnknownEvent: a camelCase on* prop on a built-in element that is
	// not a React DOM event.
	CodeUnknownEvent Code = "KR5001"
	// CodeLowercaseEvent: a lowercase on* prop (onclick) given a function.
	CodeLowercaseEvent Code = "KR5002"
	// CodeStringRef: ref="name" (string refs were removed in React 19).
	CodeStringRef Code = "KR5003"
	// CodeReadOnlyControlled: value/checked without onChange or readOnly.
	CodeReadOnlyControlled Code = "KR5004"
	// CodeValueAndDefault: value with defaultValue, or checked with defaultChecked.
	CodeValueAndDefault Code = "KR5005"
	// CodeControlledSwitch: value/checked that can switch between a defined
	// value and undefined.
	CodeControlledSwitch Code = "KR5006"
	// CodeContentEditableChange: onChange on a contentEditable element.
	CodeContentEditableChange Code = "KR5007"
)

func init() {
	Register(
		CodeInfo{
			Code:  CodeUnknownEvent,
			Title: "Unknown event handler prop",
			Explanation: "React ignores on* props on built-in elements that are not React DOM events (and warns). " +
				"Krate still attaches a native listener for the lowercased event name. Fix the spelling, or " +
				"listen for a custom DOM event with ref.current.addEventListener in an effect.",
		},
		CodeInfo{
			Code:  CodeLowercaseEvent,
			Title: "Lowercase event handler prop",
			Explanation: "React event props are camelCase (onClick, onChange). A lowercase prop such as onclick " +
				"is not an event handler in React and the function is never called. Use the camelCase name.",
		},
		CodeInfo{
			Code:  CodeStringRef,
			Title: "String ref",
			Explanation: "String refs (ref=\"name\") were removed in React 19 and are ignored. Use useRef or " +
				"createRef and pass the object (ref={inputRef}), or pass a callback ref.",
		},
		CodeInfo{
			Code:  CodeReadOnlyControlled,
			Title: "Controlled form field without onChange",
			Explanation: "A form field with value (or checked) is controlled: after every edit the DOM is restored " +
				"to the prop, so without an onChange handler it is read-only (React warns). Add onChange, use " +
				"defaultValue/defaultChecked for an uncontrolled field, or mark it readOnly.",
		},
		CodeInfo{
			Code:  CodeValueAndDefault,
			Title: "Both value and defaultValue",
			Explanation: "A form field must be either controlled (value/checked) or uncontrolled " +
				"(defaultValue/defaultChecked), not both. The default is ignored; remove one of the props.",
		},
		CodeInfo{
			Code:  CodeControlledSwitch,
			Title: "Form field switches between controlled and uncontrolled",
			Explanation: "value={cond ? x : undefined} makes the field uncontrolled while the expression is " +
				"undefined and controlled otherwise; React warns when it switches. Use an empty string (or false " +
				"for checked) instead of undefined so the field stays controlled.",
		},
		CodeInfo{
			Code:  CodeContentEditableChange,
			Title: "onChange on a contentEditable element",
			Explanation: "React's onChange only fires for input, textarea and select. A contentEditable element " +
				"never receives it; listen with onInput instead.",
		},
	)
}
