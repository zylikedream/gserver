package util

// ListDeleteFunc 删除切片中第一个满足条件的元素
func ListDeleteFunc[T any](list []T, fun func(item T) bool) []T {
	for i := range list {
		if fun(list[i]) {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	return list
}

// ListDelete 删除切片中第一个等于指定元素的元素
func ListDelete[T comparable](list []T, element T) []T {
	return ListDeleteFunc(list, func(item T) bool {
		return item == element
	})
}

// ListMemberFunc 检查切片中是否存在满足条件的元素
func ListMemberFunc[T any](list []T, fun func(item T) bool) bool {
	for _, item := range list {
		if fun(item) {
			return true
		}
	}
	return false
}

// ListMember 检查切片中是否存在指定元素
func ListMember[T comparable](list []T, element T) bool {
	return ListMemberFunc(list, func(item T) bool {
		return item == element
	})
}

// ListFindFunc 查找切片中第一个满足条件的元素，返回元素值和是否找到
func ListFindFunc[T any](list []T, fun func(item T) bool) (T, int) {
	for i, item := range list {
		if fun(item) {
			return item, i
		}
	}
	var zero T
	return zero, -1
}

// ListFind 查找切片中第一个等于指定元素的元素，返回元素值和是否找到
func ListFind[T comparable](list []T, element T) (T, int) {
	return ListFindFunc(list, func(item T) bool {
		return item == element
	})
}
