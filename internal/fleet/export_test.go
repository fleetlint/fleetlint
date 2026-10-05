package fleet

// ActionsHTMLForTest exposes the renderer to the external test package.
func ActionsHTMLForTest(name string, md []byte) []byte { return actionsHTML(name, md) }
