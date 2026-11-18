def outer
  a = 1
  [1].each do |i|
    [2].each do |j|
      puts a + i + j
    end
  end
end
outer

def twice
  yield 1
  yield 2
end
twice { |v| puts v * 2 }

def with_block(&b)
  b.call(5)
end
with_block { |v| puts v + 1 }
