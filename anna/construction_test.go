package anna

func mustClient(config Config) *Client {
	c, err := NewClient(config)
	if err != nil {
		panic(err)
	}
	return c
}
