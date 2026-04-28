# CLI (Command Line)

The command line is how you talk directly to your computer without clicking.
Instead of opening folders with a mouse, you type commands to move, create, copy, and run things.

---

## Core Skills

### Navigation
```bash
pwd                        # show where you are right now
ls                         # list files in current folder
cd google-maps-scraper     # go into a folder
cd ..                      # go back one level
cd /c/Users/ASUS/Desktop   # go to an exact path
```

### File Operations
```bash
mkdir myfolder             # create a new folder
touch queries.txt          # create an empty file (Git Bash)
cat queries.txt            # print file contents to screen
cp results.csv backup.csv  # copy a file
mv results.csv data/       # move a file into a folder
rm oldfile.csv             # delete a file (careful, no undo)
```

### Running Programs
```bash
docker run ...             # run a docker container
python script.py           # run a Python script
./myscript.sh              # run a shell script
```

### Useful Tricks
```bash
clear                      # clear the screen
CTRL+C                     # stop a running command
CTRL+Z                     # pause a running command
up arrow                   # go back to previous command
history                    # see all past commands
```

---

## PowerShell vs Git Bash (Windows)

| Task | PowerShell | Git Bash |
|------|-----------|---------|
| List files | `dir` or `ls` | `ls` |
| Go to folder | `cd folder` | `cd folder` |
| Create file | `New-Item file.txt` | `touch file.txt` |
| Print file | `Get-Content file.txt` | `cat file.txt` |
| Docker volumes | Use PowerShell (paths work correctly) | Avoid — breaks `/path` mounting |

> Always use **PowerShell** for running Docker commands on Windows.

---

## Common Mistakes

- Forgetting `cd` before running a command — you must be in the right folder
- Using Git Bash for Docker volume mounts — paths get broken
- Deleting a file with `rm` — there is no recycle bin, it is gone

---

## Practice Tasks

1. Open PowerShell and navigate to `C:\Users\ASUS\Desktop\Projects\google-maps-scraper`
2. List the files inside
3. Open `queries.txt` with `cat queries.txt`
4. Create a new file called `test.txt`
5. Delete it with `rm test.txt`
