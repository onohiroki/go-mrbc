x = 1
x += 4
puts x

$g = nil
$g ||= 7
puts $g
$g &&= $g + 1
puts $g

a = [1]
a[0] = 9
puts a[0]
