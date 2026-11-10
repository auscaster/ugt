#include <stdio.h>
#ifndef COUNT
#define COUNT 7
#endif
__attribute__((noinline)) int greenhouse_count(int trays, int pots) {
    return trays * pots + COUNT;
}
int main(int argc, char **argv) {
    printf("Greenhouse inventory: %d\n", greenhouse_count(argc, 3));
    return 0;
}
