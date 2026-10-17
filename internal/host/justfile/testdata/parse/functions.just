set unstable

greet(name) := f"Hello, {{ name }}!"
add(a, b) := a + b

hello:
    echo '{{ greet("World") }}' {{ add("1", "2") }}
