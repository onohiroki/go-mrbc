def f(a, b = 1, k:, m: 2, **rest, &blk)
  puts a
  puts b
  puts k
  puts m
  puts rest.length
  yield if blk
end

f(10, k: 5)
f(10, 20, k: 5, m: 6, z: 7) { puts "block" }

h = { k: 9, q: 1 }
f(1, **h)

l = ->(x:, y: 2) { puts x; puts y }
l.call(x: 3)
l.call(x: 3, y: 4)

[1].each do |v, w: 5|
  puts v
  puts w
end
