# random-fill

`random-fill` is a simple tool to fill random data into a file to overwrite the free space on a disk,
it works like macOS `diskutil secureErase freespace` or Linux `sfill` in `secure-delete` package.

If you want to wipe your whole disk, please use `diskutil secureErase` (without `freespace` option) or `shred`.

Keep in mind, `random-fill` is not exactly the same as these tools. `random-fill` is working for a speed purpose: 
it will write as fast as it can and the random data may be re-used during filling.


## Details

`random-fill` uses a big random data pool to provide random data.
The file is filled by the data from the random data pool.
The slower your CPU/CSPRNG is, the more times the random data will be used. 

## Usage

```
$ random-fill
Usage: ./random-fill {count} {file} [size]

(the [size] is for development purpose, it shouldn't be used by end users)

$ random-fill 2 /tmp/test.dat 1000000000
2021/12/30 11:35:27 fill 2 times to file: /tmp/test.dat, disk avail=353.95 GiB
2021/12/30 11:35:27 fill up to 953.67 MiB bytes
2021/12/30 11:35:27 fill step 1, write to /tmp/test.dat
step: 1, speed: 621.42 MiB/s (written: 926.97 MiB) ...
2021/12/30 11:35:29 fill step 1 written 953.68 MiB bytes to /tmp/test.dat
2021/12/30 11:35:29 fill step 1 removes file /tmp/test.dat
2021/12/30 11:35:29 fill step 2, write to /tmp/test.dat
step: 2, speed: 570.97 MiB/s (written: 890.41 MiB) ...
2021/12/30 11:35:31 fill step 2 written 953.68 MiB bytes to /tmp/test.dat
2021/12/30 11:35:31 fill step 2 (final) keeps file /tmp/test.dat
```
