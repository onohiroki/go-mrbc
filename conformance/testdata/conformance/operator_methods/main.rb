# Arithmetic and comparison operators are real methods. The opcodes may take
# a fast path, but alias_method, send, method_defined?, and respond_to? must
# keep seeing the builtin implementation. This case is shared with go-mrbvm
# and deliberately avoids asserting the result of an opcode after a
# redefinition, because mruby 4.0.0 and CRuby differ there.

class Integer
  alias_method :__add_before_test, :+
end
begin
  p 7.send(:+, 2)
  p Integer.method_defined?(:+)
  p 7.respond_to?(:+)
  class Integer
    def +(other); [:add, self, other]; end
  end
  p 7.send(:+, 2)
ensure
  Integer.class_eval do
    alias_method :+, :__add_before_test
    remove_method :__add_before_test
  end
end
p 7.send(:+, 2)

class Float
  alias_method :__mul_before_test, :*
end
begin
  p 1.5.send(:*, 2.0)
  p Float.method_defined?(:*)
  class Float
    def *(other); [:mul, self, other]; end
  end
  p 1.5.send(:*, 2.0)
ensure
  Float.class_eval do
    alias_method :*, :__mul_before_test
    remove_method :__mul_before_test
  end
end
p 1.5.send(:*, 2.0)
