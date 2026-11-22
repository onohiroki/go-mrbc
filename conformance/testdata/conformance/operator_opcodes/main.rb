# Arithmetic and comparison operators use dedicated opcodes. The VM falls
# back to method dispatch for non-integer receivers and user-defined methods.

class Box
  def initialize(value)
    @value = value
  end

  def +(other)
    Box.new(@value + other)
  end

  def value
    @value
  end
end

puts 1 + 2
puts 10 - 3
puts 4 * 5
puts 7 / 2
puts 1 == 1
puts 1 == 2
puts 1 != 1
puts 1 < 2
puts 2 <= 2
puts 3 > 2
puts 3 >= 3

puts 1.5 + 2.25
puts 7.5 - 2.25
puts 1.5 * 2
puts 7.5 / 2.5
puts 1.5 < 2
puts "a" + "b"
puts "ab" * 2

puts :a == :a
puts :a == :b
puts :a == 1
puts nil == nil
puts nil == false
puts true == true
puts false == false

# NaN self-equality differs between mruby 4.0.0 and CRuby, so the shared
# corpus does not assert it. See docs/compatibility-report.md.
inf = 1.0 / 0.0
puts inf == inf
puts inf < 0.0
puts 0.0 < inf

box = Box.new(10)
puts (box + 5).value
puts (box + 1.5).value

big = 300
puts big + 255
puts big - 255
puts big + 256
puts big + -1
puts big - -1

x = 5
x += 1
x -= 2
puts x

def bump(n)
  x = n
  x += 3
  x -= 1
  x
end
puts bump(10)

total = 0
[1, 2, 3].each do |i|
  total += i
end
puts total

def closure
  x = 1
  increment = -> { x += 1 }
  increment.call
  increment.call
  x
end
puts closure

a = [10, 20, 30]
puts a[0]
puts a[1]
a[1] = 99
puts a[1]
i = 2
puts a[i]
assigned = (a[0] = 7)
puts assigned
puts a[0]

class Weird
  def [](index)
    index * 2
  end

  def []=(index, value)
    :ignored
  end
end

weird = Weird.new
puts weird[3]
puts (weird[1] = 42)

h = {}
h[:x] = 1
puts h[:x]

begin
  1 / 0
rescue ZeroDivisionError => e
  puts e.class
end
