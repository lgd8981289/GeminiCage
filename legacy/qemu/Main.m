#import <Foundation/Foundation.h>
#import <unistd.h>
int main(void) {
    @autoreleasepool {
        NSString *script = [[NSBundle mainBundle] pathForResource:@"run" ofType:@"py"];
        execl("/usr/bin/python3", "python3", "-I", "-B", script.fileSystemRepresentation, (char *)NULL);
        return 1;
    }
}
