package main

type Service struct {
}

// @api
func (s *Service) ParamArgs(status int, anys ...int) (string, error) {
	return "", nil
}

func (s *Service) ParamArg(status int) (string, error) {
	return "", nil
}
