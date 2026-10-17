define BODY  
first line  



last line	
endef

app: CFLAGS = -g  
app: ; @echo [$(CFLAGS)] [$(BODY)]

test:
	echo trailing   
	
	echo after an empty recipe line
.RECIPEPREFIX = >
later:
> echo with another prefix   
>
