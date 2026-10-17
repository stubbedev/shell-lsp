# a comment with trailing spaces   
   # an indented comment	
# a comment ending in an escaped space \ 
A = yes
ifeq ($(A),yes)   
  B = 1
else ifdef C  
  B = 2
else	
  B = 3
endif  # done   
ifndef D 
D = 4
endif
vpath %.c src   
undefine C  
$(info $(B) $(D))   
all: ; @:
